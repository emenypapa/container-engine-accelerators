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

package metrics

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	//"github.com/NVIDIA/go-nvml/pkg/nvml"
	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type HostMonitor struct {
	tpuDesc *prometheus.Desc
}

func NewHostMonitor() *HostMonitor {
	return &HostMonitor{
		tpuDesc: prometheus.NewDesc(
			"usage_rate_tpu_node",
			"Percent of time when the TPU was actively processing",
			//动态标签key列表
			[]string{"instance_id", "instance_name"},
			//静态标签
			prometheus.Labels{"module": "cpu"},
		),
	}
}

func (h *HostMonitor) Describe(ch chan<- *prometheus.Desc) {
	ch <- h.tpuDesc
}

func (h *HostMonitor) Collect(ch chan<- prometheus.Metric) {
	reg := regexp.MustCompile(deviceRE)
	files, err := ioutil.ReadDir(tpuSysfsPath)
	if err != nil {
		glog.Errorf("Failed to get device for %s: %v", tpuSysfsPath, err)
		return
	}
	var value int
	for _, f := range files {
		if f.IsDir() {
			if reg.MatchString(f.Name()) {
				glog.V(3).Infof("Found Eicas TPU %q\n", f.Name())
				usage, err := h.usageAnalysis(path.Join(tpuSysfsPath, f.Name(), "device", "npu_usage"))
				if err != nil {
					continue
				}
				value = usage
				//UsageRateNodeTpu.WithLabelValues("eicas", "mi.uuid", "mi.deviceModel").Set(float64(usage))
			}
		} else {
			continue
		}

	}
	ch <- prometheus.MustNewConstMetric(h.tpuDesc, prometheus.GaugeValue, float64(value))

}

//type metricsCollector interface {
//	collectGPUDevice(deviceName string) (*nvml.Device, error)
//	collectDutyCycle(string, time.Duration) (uint, error)
//	collectGpuMetricsInfo(device string, d *nvml.Device) (metricsInfo, error)
//}
//
//var gmc metricsCollector

//type mCollector struct{}
//
//type metricsInfo struct {
//	usageRate   uint
//	dutyCycle   uint
//	usedMemory  uint64
//	totalMemory uint64
//	uuid        string
//	deviceModel string
//}

//func (t *mCollector) collectGPUDevice(deviceName string) (*nvml.Device, error) {
//	return DeviceFromName(deviceName)
//}

//func (t *mCollector) collectDutyCycle(uuid string, since time.Duration) (uint, error) {
//	return AverageGPUUtilization(uuid, since)
//}

//func (t *mCollector) collectGpuMetricsInfo(device string, d *nvml.Device) (metricsInfo, error) {
//	return getGpuMetricsInfo(device, d)
//}

var (
	// UsageRateNodeTpu reports the percent of time when the TPU was actively processing per Node.
	UsageRateNodeTpu = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "usage_rate_tpu_node",
			Help: "Percent of time when the TPU was actively processing",
		},
		[]string{"make", "accelerator_id", "model"})
)

const metricsResetInterval = time.Minute

// MetricServer exposes GPU metrics for all containers and nodes in prometheus format on the specified port.
type MetricServer struct {
	collectionInterval   int
	port                 int
	metricsEndpointPath  string
	lastMetricsResetTime time.Time
}

func NewMetricServer(collectionInterval, port int, metricsEndpointPath string) *MetricServer {
	return &MetricServer{
		collectionInterval:   collectionInterval,
		port:                 port,
		metricsEndpointPath:  metricsEndpointPath,
		lastMetricsResetTime: time.Now(),
	}
}

// Start performs necessary initializations and starts the metric server.
func (m *MetricServer) Start() error {
	glog.Infoln("Starting metrics server")

	//校验nvml可用性，是否存在设备
	//driverVersion, ret := nvml.SystemGetDriverVersion()
	//if ret != nvml.SUCCESS {
	//	return fmt.Errorf("failed to query nvml: %v", nvml.ErrorString(ret))
	//}
	//glog.Infof("nvml initialized successfully. Driver version: %s", driverVersion)
	//
	//err := DiscoverGPUDevices()
	//if err != nil {
	//	return fmt.Errorf("failed to discover GPU devices: %v", err)
	//}

	go func() {
		registry := prometheus.NewRegistry()
		registry.MustRegister(NewHostMonitor())
		http.Handle(m.metricsEndpointPath, promhttp.Handler())
		err := http.ListenAndServe(fmt.Sprintf(":%d", m.port), nil)
		if err != nil {
			glog.Infof("Failed to start metric server: %v", err)
		}
	}()

	//go m.collectMetrics()
	return nil
}

func (m *MetricServer) collectMetrics() {
	//gmc = &mCollector{}
	t := time.NewTicker(time.Millisecond * time.Duration(m.collectionInterval))
	defer t.Stop()

	for {
		select {
		case <-t.C:
			m.updateMetrics()
		}
	}
}

//func getGpuMetricsInfo(device string, d *nvml.Device) (metricsInfo, error) {
//	uuid, ret := d.GetUUID()
//	if ret != nvml.SUCCESS {
//		return metricsInfo{}, fmt.Errorf("failed to get GPU UUID: %v", nvml.ErrorString(ret))
//	}
//	deviceModel, ret := d.GetName()
//	if ret != nvml.SUCCESS {
//		return metricsInfo{}, fmt.Errorf("failed to get GPU device model: %v", nvml.ErrorString(ret))
//	}
//
//	mem, ret := d.GetMemoryInfo()
//	if ret != nvml.SUCCESS {
//		return metricsInfo{}, fmt.Errorf("failed to get GPU memory: %v", nvml.ErrorString(ret))
//	}
//	dutyCycle, err := gmc.collectDutyCycle(uuid, time.Second*10)
//	if err != nil {
//		return metricsInfo{}, fmt.Errorf("failed to get dutyCycle: %v", err)
//	}
//	return metricsInfo{
//		dutyCycle:   dutyCycle,
//		usedMemory:  mem.Used,
//		totalMemory: mem.Total,
//		uuid:        uuid,
//		deviceModel: deviceModel}, nil
//}

func (m *MetricServer) updateMetrics() {
	//m.resetMetricsIfNeeded()
	//reg := regexp.MustCompile(deviceRE)
	//files, err := ioutil.ReadDir(tpuSysfsPath)
	//if err != nil {
	//	glog.Errorf("Failed to get device for %s: %v", tpuSysfsPath, err)
	//	return
	//}
	//for _, f := range files {
	//	if f.IsDir() {
	//		if reg.MatchString(f.Name()) {
	//			glog.V(3).Infof("Found Eicas TPU %q\n", f.Name())
	//			usage, err := m.usageAnalysis(path.Join(tpuSysfsPath, f.Name(), "device", "npu_usage"))
	//			if err != nil {
	//				continue
	//			}
	//			UsageRateNodeTpu.WithLabelValues("eicas", "mi.uuid", "mi.deviceModel").Set(float64(usage))
	//		}
	//	} else {
	//		continue
	//	}
	//
	//}
}

func (h *HostMonitor) usageAnalysis(fileName string) (usage int, err error) {
	file, err := os.Open(fileName)
	if err != nil {
		glog.Infof("Failed to usageAnalysis: %v", err)
		return 0, err
	}
	defer file.Close()

	// 创建一个 Scanner 用于读取文件内容
	scanner := bufio.NewScanner(file)

	// 逐行读取文件内容
	for scanner.Scan() {
		line := scanner.Text()
		// 查找包含 "usage" 的行
		if strings.Contains(line, "usage") {
			// 解析出数值部分
			fields := strings.Split(line, " ")
			if len(fields) >= 2 {
				valueStr := strings.TrimSpace(fields[0])
				values := strings.Split(valueStr, ":")
				value, err := strconv.Atoi(values[1])
				if err != nil {
					glog.Infof("Failed to usageAnalysis: %v", err)
					return 0, err
				} else {
					// 输出解析出的数值
					glog.Infof("Success to usageAnalysis: %v", value)
					return value, nil
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		glog.Infof("Failed to usageAnalysis: %v", err)
		return 0, err
	}
	return 0, nil
}

func (m *MetricServer) resetMetricsIfNeeded() {
	if time.Now().After(m.lastMetricsResetTime.Add(metricsResetInterval)) {
		UsageRateNodeTpu.Reset()

		m.lastMetricsResetTime = time.Now()
	}
}

// Stop performs cleanup operations and stops the metric server.
func (m *MetricServer) Stop() {
}
