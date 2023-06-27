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

package main

import (
	"bytes"
	"fmt"
	"log"
	"os/exec"
)

const (
	// Device plugin settings.
	kubeletEndpoint                = "kubelet.sock"
	pluginEndpointPrefix           = "eicasTPU"
	devDirectory                   = "/proc/bmsophon"
	hostPathPrefix                 = "/opt/sophon"
	containerPathPrefix            = "/opt/sophon"
	enableContainerGPUMetrics      = true
	tpuMetricsPort                 = 2112
	tpuMetricsCollectionIntervalMs = 30000
)

func main() {

	cmd := exec.Command("bm-smi", "|", "grep", "PID")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout // 标准输出
	cmd.Stderr = &stderr // 标准错误
	err := cmd.Run()
	outStr, errStr := string(stdout.Bytes()), string(stderr.Bytes())
	fmt.Printf("out:\n%s\nerr:\n%s\n", outStr, errStr)
	if err != nil {
		log.Fatalf("cmd.Run() failed with %s\n", err)
	}

	//flag.Parse()
	//glog.Infoln("device-plugin started")
	//mountPaths := []pluginapi.Mount{
	//	{HostPath: hostPathPrefix, ContainerPath: containerPathPrefix, ReadOnly: true}}
	//
	//ngm := gpumanager.NewEicasTPUManager(devDirectory, mountPaths)
	//
	//for {
	//	err := ngm.CheckDevicePaths()
	//	if err == nil {
	//		break
	//	}
	//	// Use non-default level to avoid log spam.
	//	glog.V(3).Infof("eicasTPUManager.CheckDevicePaths() failed: %v", err)
	//	time.Sleep(5 * time.Second)
	//}
	//
	//for {
	//	err := ngm.Start()
	//	if err == nil {
	//		break
	//	}
	//
	//	glog.Errorf("failed to start TPU device manager: %v", err)
	//	time.Sleep(5 * time.Second)
	//}
	//
	//if enableContainerGPUMetrics {
	//	glog.Infof("Starting metrics server on port: %d, endpoint path: %s, collection frequency: %d", tpuMetricsPort, "/metrics", tpuMetricsCollectionIntervalMs)
	//	metricServer := metrics.NewMetricServer(tpuMetricsCollectionIntervalMs, tpuMetricsPort, "/metrics")
	//	err := metricServer.Start()
	//	if err != nil {
	//		glog.Infof("Failed to start metric server: %v", err)
	//		return
	//	}
	//}
	//
	//ngm.Serve(pluginapi.DevicePluginPath, kubeletEndpoint, fmt.Sprintf("%s-%d.sock", pluginEndpointPrefix, time.Now().Unix()))
}
