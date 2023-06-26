// Copyright 2017 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package nvidia

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path"
	"regexp"
	"sync"
	"time"

	//"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/golang/glog"
	"google.golang.org/grpc"

	pluginapi "k8s.io/kubelet/pkg/apis/deviceplugin/v1beta1"
)

const (
	eicasDeviceRE             = `^card[0-9]*$`
	tpuCheckInterval          = 10 * time.Second
	pluginSocketCheckInterval = 1 * time.Second
)

var (
	resourceName = "eicas.com/tpu"
)

// eicasTPUManager manages eicas tpu devices.
type eicasTPUManager struct {
	devDirectory string
	mountPaths   []pluginapi.Mount
	devices      map[string]pluginapi.Device
	grpcServer   *grpc.Server
	socket       string
	stop         chan bool
	devicesMutex sync.Mutex
	Health       chan pluginapi.Device
}

func NewEicasTPUManager(devDirectory string, mountPaths []pluginapi.Mount) *eicasTPUManager {
	return &eicasTPUManager{

		devDirectory: devDirectory,
		mountPaths:   mountPaths,
		devices:      make(map[string]pluginapi.Device),
		stop:         make(chan bool),
		Health:       make(chan pluginapi.Device),
	}
}

func (etm *eicasTPUManager) ListPhysicalDevices() map[string]pluginapi.Device {
	return etm.devices
}

func (etm *eicasTPUManager) ListDevices() map[string]pluginapi.Device {
	physicalGPUDevices := etm.ListPhysicalDevices()
	return physicalGPUDevices
}

func (etm *eicasTPUManager) DeviceSpec(deviceID string) ([]pluginapi.DeviceSpec, error) {
	deviceSpecs := make([]pluginapi.DeviceSpec, 0)
	dev, ok := etm.devices[deviceID]
	if !ok {
		return deviceSpecs, fmt.Errorf("invalid allocation request with non-existing device %s", deviceID)
	}
	if dev.Health != pluginapi.Healthy {
		return deviceSpecs, fmt.Errorf("invalid allocation request with unhealthy device %s", deviceID)
	}
	deviceSpecs = append(deviceSpecs, pluginapi.DeviceSpec{
		HostPath:      path.Join(etm.devDirectory, deviceID),
		ContainerPath: path.Join(etm.devDirectory, deviceID),
		Permissions:   "mrw",
	})
	return deviceSpecs, nil
}

func (etm *eicasTPUManager) discoverTPUs() error {
	reg := regexp.MustCompile(eicasDeviceRE)
	files, err := ioutil.ReadDir(etm.devDirectory)
	if err != nil {
		return err
	}
	for _, f := range files {
		if f.IsDir() {
			if reg.MatchString(f.Name()) {
				glog.V(3).Infof("Found Eicas TPU %q\n", f.Name())
				etm.SetDeviceHealth(f.Name(), pluginapi.Healthy)
			}
		} else {
			continue
		}

	}
	return nil
}

func (etm *eicasTPUManager) hasAdditionalTPUsInstalled() bool {
	etm.devicesMutex.Lock()
	originalDeviceCount := len(etm.devices)
	etm.devicesMutex.Unlock()
	deviceCount, err := etm.discoverNumTPUs()
	if err != nil {
		glog.Errorln(err)
		return false
	}
	if deviceCount > originalDeviceCount {
		glog.Infof("Found %v TPUs, while only %v are registered. Stopping device-plugin server.", deviceCount, originalDeviceCount)
		return true
	}
	return false
}

func (etm *eicasTPUManager) discoverNumTPUs() (int, error) {
	reg := regexp.MustCompile(eicasDeviceRE)
	deviceCount := 0
	files, err := ioutil.ReadDir(etm.devDirectory)
	if err != nil {
		return deviceCount, err
	}
	for _, f := range files {
		if f.IsDir() {
			if reg.MatchString(f.Name()) {
				deviceCount++
			}
		} else {
			continue
		}

	}
	return deviceCount, nil
}

// SetDeviceHealth sets the health status for a GPU device or partition if MIG is enabled
func (etm *eicasTPUManager) SetDeviceHealth(name string, health string) {
	etm.devicesMutex.Lock()
	defer etm.devicesMutex.Unlock()

	reg := regexp.MustCompile(eicasDeviceRE)
	if reg.MatchString(name) {
		etm.devices[name] = pluginapi.Device{ID: name, Health: health}
	}
}

func (etm *eicasTPUManager) CheckDevicePaths() error {
	if _, err := os.Stat(etm.devDirectory); err != nil {
		return err
	}
	return nil
}

func (etm *eicasTPUManager) Start() error {
	//设备发现
	if err := etm.discoverTPUs(); err != nil {
		return err
	}
	return nil
}

func (etm *eicasTPUManager) Serve(pMountPath, kEndpoint, pluginEndpoint string) {
	registerWithKubelet := false
	if _, err := os.Stat(path.Join(pMountPath, kEndpoint)); err == nil {
		glog.Infof("will use alpha API\n")
		registerWithKubelet = true
	} else {
		glog.Infof("will use beta API\n")
	}

	for {
		select {
		case <-etm.stop:
			close(etm.stop)
			return
		default:
			{
				pluginEndpointPath := path.Join(pMountPath, pluginEndpoint)
				glog.Infof("starting device-plugin server at: %s\n", pluginEndpointPath)
				lis, err := net.Listen("unix", pluginEndpointPath)
				if err != nil {
					glog.Fatalf("starting device-plugin server failed: %v", err)
				}
				etm.socket = pluginEndpointPath
				etm.grpcServer = grpc.NewServer()

				// Registers the supported versions of service.
				pluginbeta := &pluginServiceV1Beta1{etm: etm}
				pluginbeta.RegisterService()

				var wg sync.WaitGroup
				wg.Add(1)
				// Starts device plugin service.
				go func() {
					defer wg.Done()
					// Blocking call to accept incoming connections.
					err := etm.grpcServer.Serve(lis)
					glog.Errorf("device-plugin server stopped serving: %v", err)
				}()

				if registerWithKubelet {
					// Wait till the grpcServer is ready to serve services.
					for len(etm.grpcServer.GetServiceInfo()) <= 0 {
						time.Sleep(1 * time.Second)
					}
					glog.Infoln("device-plugin server started serving")
					// Registers with Kubelet.
					err = RegisterWithV1Beta1Kubelet(path.Join(pMountPath, kEndpoint), pluginEndpoint, resourceName)
					if err != nil {
						etm.grpcServer.Stop()
						wg.Wait()
						glog.Fatal(err)
					}
					glog.Infoln("device-plugin registered with the kubelet")
				}

				// This is checking if the plugin socket was deleted
				// and also if there are additional GPU devices installed.
				// If so, stop the grpc server and start the whole thing again.
				gpuCheck := time.NewTicker(tpuCheckInterval)
				pluginSocketCheck := time.NewTicker(pluginSocketCheckInterval)
				defer gpuCheck.Stop()
				defer pluginSocketCheck.Stop()
			statusCheck:
				for {
					select {
					case <-pluginSocketCheck.C:
						if _, err := os.Lstat(pluginEndpointPath); err != nil {
							glog.Infof("stopping device-plugin server at: %s\n", pluginEndpointPath)
							glog.Errorln(err)
							etm.grpcServer.Stop()
							break statusCheck
						}
					case <-gpuCheck.C:
						if etm.hasAdditionalTPUsInstalled() {
							etm.grpcServer.Stop()
							for {
								err := etm.discoverTPUs()
								if err == nil {
									break statusCheck
								}
							}
						}

					}
				}
				wg.Wait()
			}
		}
	}
}

func (etm *eicasTPUManager) Stop() error {
	glog.Infof("removing device plugin socket %s\n", etm.socket)
	if err := os.Remove(etm.socket); err != nil && !os.IsNotExist(err) {
		return err
	}
	etm.stop <- true
	<-etm.stop
	close(etm.Health)
	return nil
}
