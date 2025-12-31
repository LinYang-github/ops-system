package monitor

import (
	"fmt"
	"strconv"
	"sync"
	"time"
)

// Point 数据点 [时间戳, 值]
type Point struct {
	Time  int64
	Value float64
}

// MemoryTSDB 内存时序存储
type MemoryTSDB struct {
	// key 格式: "metric_name|instance_ip" (e.g. "cpu_usage|192.168.1.10")
	series map[string][]Point
	mu     sync.RWMutex

	retention time.Duration // 数据保留时长 (10分钟)
}

func NewMemoryTSDB() *MemoryTSDB {
	return &MemoryTSDB{
		series:    make(map[string][]Point),
		retention: 10 * time.Minute,
	}
}

// Write 写入数据
func (tsdb *MemoryTSDB) Write(ip, metric string, val float64) {
	key := fmt.Sprintf("%s|%s", metric, ip)
	now := time.Now().Unix()

	tsdb.mu.Lock()
	defer tsdb.mu.Unlock()

	// 1. 追加新点
	if _, ok := tsdb.series[key]; !ok {
		tsdb.series[key] = make([]Point, 0, 300)
	}
	tsdb.series[key] = append(tsdb.series[key], Point{Time: now, Value: val})

	// 2. 修剪旧数据 (简单的滑动窗口)
	// 保留最近 10 分钟的数据。假设 3秒一个点，10分钟约 200个点。
	// 为了性能，不每次都遍历，当长度超过 300 时清理一次
	data := tsdb.series[key]
	if len(data) > 300 {
		cutoff := now - int64(tsdb.retention.Seconds())
		validIdx := 0
		for i, p := range data {
			if p.Time >= cutoff {
				validIdx = i
				break
			}
		}
		// 切片操作，丢弃前面的
		tsdb.series[key] = data[validIdx:]
	}
}

// QueryRange 模拟 Prometheus 查询
// 返回结构适配 Prometheus Matrix 格式
func (tsdb *MemoryTSDB) QueryRange(metric, ip string, start, end int64) []Point {
	key := fmt.Sprintf("%s|%s", metric, ip)

	tsdb.mu.RLock()
	defer tsdb.mu.RUnlock()

	rawData, ok := tsdb.series[key]
	if !ok {
		return []Point{}
	}

	// 过滤时间范围
	var result []Point
	for _, p := range rawData {
		if p.Time >= start && p.Time <= end {
			result = append(result, p)
		}
	}
	return result
}

// FormatPrometheusResponse 将内部 Point 转换为 Prometheus JSON 结构
// Prometheus Value 是 [timestamp, "string_value"]
func FormatPrometheusResponse(metric, id string, points []Point) map[string]interface{} {
	values := make([][]interface{}, len(points))
	for i, p := range points {
		// Prometheus API 返回的时间戳是秒(浮点)或毫秒，这里用秒
		// 值必须是字符串
		values[i] = []interface{}{p.Time, strconv.FormatFloat(p.Value, 'f', 2, 64)}
	}

	return map[string]interface{}{
		"status": "success",
		"data": map[string]interface{}{
			"resultType": "matrix",
			"result": []interface{}{
				map[string]interface{}{
					"metric": map[string]string{
						"__name__": metric,
						"instance": id,
					},
					"values": values,
				},
			},
		},
	}
}
