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
	"encoding/json"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/golang/glog"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

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

func (m *MetricServer) RunHttpServer() {
	r := gin.New()
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	api := r.Group(m.metricsEndpointPath)
	{
		api.GET("/tpu_usage", m.TpuUsageController)
		api.GET("/tpu_mem", m.TpuMemController)
	}

	_ = r.Run(fmt.Sprintf(":%d", m.port))
}

func (m *MetricServer) TpuUsageController(ctx *gin.Context) {
	appG := Gin{C: ctx}

	usage, err := m.TpuUsage()

	if err != nil {
		appG.ResponseError(InvalidParams, err.Error())
		return
	}

	appG.Response(http.StatusOK, SUCCESS, usage)
	return
}

func (m *MetricServer) TpuMemController(ctx *gin.Context) {
	appG := Gin{C: ctx}

	data, err := m.TpuMem()

	if err != nil {
		appG.ResponseError(InvalidParams, err.Error())
		return
	}

	appG.Response(http.StatusOK, SUCCESS, data)
	return
}

// /proc/bmsophon/card0/bmsophon0/media
func (m *MetricServer) TpuMem() (val TpuMemAnalysis, err error) {
	reg := regexp.MustCompile(eicasDeviceRE)
	regbm := regexp.MustCompile(deviceRE)
	files, err := ioutil.ReadDir(tpuProcPath)
	if err != nil {
		glog.Errorf("Failed to get device for %s: %v", tpuSysfsPath, err)
		return
	}
	var total, used, free int64
	for _, f := range files {
		if f.IsDir() {
			if reg.MatchString(f.Name()) {
				bmsx, err := ioutil.ReadDir(path.Join(tpuProcPath, f.Name()))
				if err != nil {
					glog.Errorf("Failed to get device for %s: %v", path.Join(tpuProcPath, f.Name()), err)
					continue
				}
				for _, b := range bmsx {
					if b.IsDir() {
						if regbm.MatchString(b.Name()) {
							fileName := path.Join(tpuProcPath, f.Name(), b.Name(), "media")
							glog.Infof("Tpu Mem json file path: %s", fileName)
							totalMemSize, usedMemSize, freeMemSize := m.memAnalysis(fileName)
							total += totalMemSize
							used += usedMemSize
							free += freeMemSize
						}
					} else {
						continue
					}
				}
			}
		} else {
			continue
		}

	}

	val = TpuMemAnalysis{
		TotalMemSize: total,
		UsedMemSize:  used,
		FreeMemSize:  free,
	}
	return val, nil
}

func (m *MetricServer) TpuUsage() (val int, err error) {
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
				usage, err := m.usageAnalysis(path.Join(tpuSysfsPath, f.Name(), "device", "npu_usage"))
				if err != nil {
					continue
				}
				val = usage
			}
		} else {
			continue
		}

	}
	return 0, nil
}

type TpuMemAnalysis struct {
	TotalMemSize int64 `json:"total_mem_size"`
	UsedMemSize  int64 `json:"used_mem_size"`
	FreeMemSize  int64 `json:"free_mem_size"`
}

func (m *MetricServer) memAnalysis(fileName string) (totalMemSize, usedMemSize, freeMemSize int64) {
	bs, err := ioutil.ReadFile(fileName)
	if err != nil {
		glog.Infof("Failed to usageAnalysis: %v", err)
	}
	var t TpuMemAnalysis
	err = json.Unmarshal(bs, &t)

	totalMemSize = t.TotalMemSize
	usedMemSize = t.UsedMemSize
	freeMemSize = t.FreeMemSize
	return
}

func (m *MetricServer) usageAnalysis(fileName string) (usage int, err error) {
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

type Gin struct {
	C *gin.Context
}

type Response struct {
	Code int32  `json:"code"`
	Msg  string `json:"msg"`
	//RequestId string      `json:"request_id"`
	Time time.Time   `json:"time"`
	Data interface{} `json:"data"`
}

func (g *Gin) Response(httpCode, errCode int32, data interface{}) {
	SetErrorCode(g.C, errCode)
	response := Response{
		Code: errCode,
		Data: data,
		Time: time.Now(),
	}
	_, err := json.Marshal(response)
	if err != nil {
		fmt.Println("json marshal error !")
	}
	g.C.JSON(int(httpCode), response)
	return
}

func (g *Gin) ResponseError(errCode int32, data interface{}) {
	g.Response(http.StatusOK, errCode, data)
}

func SetErrorCode(ctx *gin.Context, errorCode int32) {
	SetContextData(ctx, ErrorCode, errorCode)
}

func SetContextData(ctx *gin.Context, key string, value interface{}) {
	if ctx.Keys == nil {
		ctx.Keys = make(map[string]interface{})
	}
	//ctx.Keys[key] = value
	ctx.Set(key, value)
}

const (
	UserId    = "user_id"
	Token     = "token"
	Role      = "role_code"
	Name      = "name"
	Nickname  = "nickname"
	Avatar    = "avatar"
	Email     = "email"
	Phone     = "phone"
	ErrorCode = "error_code"
	AlarmType = "alarm_type"
	RequestId = "request_id"
	LoginType = "login_type"
)

const (
	SUCCESS                = 200   // 成功
	ERROR                  = 500   // 失败
	InvalidParams          = 400   // 参数错误
	SSONotLoggedIn         = 1100  // sso未登录
	NotLoggedIn            = 1000  // 未登录
	ParameterIllegal       = 1001  // 参数不合法
	UnauthorizedUserId     = 1002  // 用户Id不合法
	Unauthorized           = 1003  // 未授权
	ServerError            = 1004  // 系统错误
	NotData                = 1005  // 没有数据
	ModelAddError          = 1006  // 添加错误
	ModelDeleteError       = 1007  // 删除错误
	ModelStoreError        = 1008  // 存储错误
	OperationFailure       = 1009  // 操作失败
	RoutingNotExist        = 1010  // 路由不存在
	ErrorUserExist         = 1011  // 用户已存在
	ErrorUserNotExist      = 1012  // 用户不存在
	ErrorNeedCaptcha       = 1013  // 需要验证码
	PasswordInvalid        = 1014  // 密码不符合规范
	ErrorCheckTokenFail    = 10001 // 用户信息获取失败
	ErrorCheckUserRoleFail = 10002 // 用户信息获取失败
	CustomIdentityInvalid  = 10010 // 身份信息不正确
	AccessTooFrequently    = 99999 // 访问太频繁
	UnScreenshot           = 10003 // 访问太频繁
	DateTooLong            = 10005 // 日期超长
)

//type HostMonitor struct {
//	tpuUsageDesc *prometheus.Desc
//}
//
//func NewHostMonitor() *HostMonitor {
//	return &HostMonitor{
//		tpuUsageDesc: prometheus.NewDesc(
//			"usage_rate_tpu_node",
//			"Percent of time when the TPU was actively processing",
//			//动态标签key列表
//			nil,
//			//静态标签
//			nil,
//		),
//	}
//}
//
//func (h *HostMonitor) Describe(ch chan<- *prometheus.Desc) {
//	ch <- h.tpuUsageDesc
//}
//
//func (h *HostMonitor) Collect(ch chan<- prometheus.Metric) {
//
//	value, err := h.tupUsage()
//	if err != nil {
//		glog.Errorf("Failed to get tpu usage for : %v", err)
//		return
//	}
//	ch <- prometheus.MustNewConstMetric(h.tpuUsageDesc, prometheus.GaugeValue, value)
//
//}

// Start performs necessary initializations and starts the metric server.
//func (m *MetricServer) Start() error {
//	glog.Infoln("Starting metrics server")
//	//TODO 校验nvml可用性，是否存在设备
//	go func() {
//		registry := prometheus.NewRegistry()
//		registry.MustRegister(NewHostMonitor())
//		http.Handle(m.metricsEndpointPath, promhttp.HandlerFor(registry, promhttp.HandlerOpts{Registry: registry}))
//		err := http.ListenAndServe(fmt.Sprintf(":%d", m.port), nil)
//		if err != nil {
//			glog.Infof("Failed to start metric server: %v", err)
//		}
//	}()
//	return nil
//}
