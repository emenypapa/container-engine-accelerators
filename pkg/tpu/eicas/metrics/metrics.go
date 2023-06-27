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
	//"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type HostMonitor struct {
	tpuUsageDesc *prometheus.Desc
}

func NewHostMonitor() *HostMonitor {
	return &HostMonitor{
		tpuUsageDesc: prometheus.NewDesc(
			"usage_rate_tpu_node",
			"Percent of time when the TPU was actively processing",
			//动态标签key列表
			nil,
			//静态标签
			nil,
		),
	}
}

func (h *HostMonitor) Describe(ch chan<- *prometheus.Desc) {
	ch <- h.tpuUsageDesc
}

func (h *HostMonitor) Collect(ch chan<- prometheus.Metric) {

	value, err := h.tupUsage()
	if err != nil {
		glog.Errorf("Failed to get tpu usage for : %v", err)
		return
	}
	ch <- prometheus.MustNewConstMetric(h.tpuUsageDesc, prometheus.GaugeValue, value)

}

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
	//TODO 校验nvml可用性，是否存在设备
	go func() {
		registry := prometheus.NewRegistry()
		registry.MustRegister(NewHostMonitor())
		http.Handle(m.metricsEndpointPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{Registry: registry}))
		err := http.ListenAndServe(fmt.Sprintf(":%d", m.port), nil)
		if err != nil {
			glog.Infof("Failed to start metric server: %v", err)
		}
	}()
	return nil
}

func (h *HostMonitor) tupUsage() (val float64, err error) {
	reg := regexp.MustCompile(deviceRE)
	files, err := ioutil.ReadDir(tpuSysfsPath)
	if err != nil {
		glog.Errorf("Failed to get device for %s: %v", tpuSysfsPath, err)
		return
	}
	for _, f := range files {
		if f.IsDir() {
			if reg.MatchString(f.Name()) {
				glog.V(3).Infof("Found Eicas TPU %q\n", f.Name())
				usage, err := h.usageAnalysis(path.Join(tpuSysfsPath, f.Name(), "device", "npu_usage"))
				if err != nil {
					continue
				}
				val = float64(usage)
				//UsageRateNodeTpu.WithLabelValues("eicas", "mi.uuid", "mi.deviceModel").Set(float64(usage))
			}
		} else {
			continue
		}

	}
	return 0, nil
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
