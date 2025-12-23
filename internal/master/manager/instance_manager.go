package manager

import (
	"database/sql"
	"sync"
	"time"

	"ops-system/pkg/protocol"
)

// 实时监控数据结构
type realTimeMetrics struct {
	CpuUsage float64
	MemUsage uint64
	IoRead   uint64
	IoWrite  uint64
}

// InstanceManager 专门负责实例管理和监控数据
type InstanceManager struct {
	db           *sql.DB
	mu           sync.Mutex
	metricsCache sync.Map // key: InstanceID, value: realTimeMetrics
}

func NewInstanceManager(db *sql.DB) *InstanceManager {
	return &InstanceManager{db: db}
}

// RegisterInstance 注册/更新实例基础信息
func (im *InstanceManager) RegisterInstance(inst *protocol.InstanceInfo) {
	im.mu.Lock()
	defer im.mu.Unlock()
	query := `INSERT OR REPLACE INTO instance_infos (id, system_id, node_ip, service_name, service_version, status, pid, uptime) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	im.db.Exec(query, inst.ID, inst.SystemID, inst.NodeIP, inst.ServiceName, inst.ServiceVersion, inst.Status, inst.PID, inst.Uptime)
}

// UpdateInstanceStatus 简单更新状态
func (im *InstanceManager) UpdateInstanceStatus(id, status string, pid int) {
	im.mu.Lock()
	defer im.mu.Unlock()

	uptime := int64(0)
	if status == "running" {
		uptime = time.Now().Unix()
	}

	if status != "running" {
		im.db.Exec(`UPDATE instance_infos SET status = ?, pid = ?, uptime = 0 WHERE id = ?`, status, pid, id)
		im.metricsCache.Delete(id) // 停止后清理缓存
	} else {
		im.db.Exec(`UPDATE instance_infos SET status = ?, pid = ?, uptime = ? WHERE id = ?`, status, pid, uptime, id)
	}
}

// UpdateInstanceFullStatus 根据 Worker 报告完整更新
func (im *InstanceManager) UpdateInstanceFullStatus(report *protocol.InstanceStatusReport) {
	// 1. DB 更新状态
	im.mu.Lock()
	im.db.Exec(`UPDATE instance_infos SET status=?, pid=?, uptime=? WHERE id=?`, report.Status, report.PID, report.Uptime, report.InstanceID)
	im.mu.Unlock()

	// 2. 内存更新监控数据
	metrics := realTimeMetrics{
		CpuUsage: report.CpuUsage,
		MemUsage: report.MemUsage,
		IoRead:   report.IoRead,
		IoWrite:  report.IoWrite,
	}
	im.metricsCache.Store(report.InstanceID, metrics)
}

// RemoveInstance 删除实例
func (im *InstanceManager) RemoveInstance(id string) {
	im.mu.Lock()
	defer im.mu.Unlock()
	im.db.Exec("DELETE FROM instance_infos WHERE id = ?", id)
	im.metricsCache.Delete(id)
}

// GetInstance 获取单个实例
func (im *InstanceManager) GetInstance(id string) (*protocol.InstanceInfo, bool) {
	var inst protocol.InstanceInfo
	err := im.db.QueryRow(`SELECT id, system_id, node_ip, service_name, service_version, status, pid, uptime FROM instance_infos WHERE id = ?`, id).
		Scan(&inst.ID, &inst.SystemID, &inst.NodeIP, &inst.ServiceName, &inst.ServiceVersion, &inst.Status, &inst.PID, &inst.Uptime)
	if err != nil {
		return nil, false
	}

	// 合并监控数据
	if val, ok := im.metricsCache.Load(id); ok {
		m := val.(realTimeMetrics)
		inst.CpuUsage = m.CpuUsage
		inst.MemUsage = m.MemUsage
		inst.IoRead = m.IoRead
		inst.IoWrite = m.IoWrite
	}
	return &inst, true
}

// GetSystemInstances 获取某个系统下的所有实例
func (im *InstanceManager) GetSystemInstances(systemID string) ([]protocol.InstanceInfo, error) {
	query := `SELECT id, system_id, node_ip, service_name, service_version, status, pid, uptime FROM instance_infos WHERE system_id = ?`
	rows, err := im.db.Query(query, systemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []protocol.InstanceInfo
	for rows.Next() {
		var i protocol.InstanceInfo
		rows.Scan(&i.ID, &i.SystemID, &i.NodeIP, &i.ServiceName, &i.ServiceVersion, &i.Status, &i.PID, &i.Uptime)

		if val, ok := im.metricsCache.Load(i.ID); ok {
			m := val.(realTimeMetrics)
			i.CpuUsage = m.CpuUsage
			i.MemUsage = m.MemUsage
			i.IoRead = m.IoRead
			i.IoWrite = m.IoWrite
		}
		instances = append(instances, i)
	}
	return instances, nil
}

// GetAllInstances 获取所有实例 (供 SystemManager 组装视图使用)
func (im *InstanceManager) GetAllInstances() map[string][]*protocol.InstanceInfo {
	instRows, _ := im.db.Query(`SELECT id, system_id, node_ip, service_name, service_version, status, pid, uptime FROM instance_infos`)
	defer instRows.Close()

	instMap := make(map[string][]*protocol.InstanceInfo)
	for instRows.Next() {
		var i protocol.InstanceInfo
		instRows.Scan(&i.ID, &i.SystemID, &i.NodeIP, &i.ServiceName, &i.ServiceVersion, &i.Status, &i.PID, &i.Uptime)

		if val, ok := im.metricsCache.Load(i.ID); ok {
			m := val.(realTimeMetrics)
			i.CpuUsage = m.CpuUsage
			i.MemUsage = m.MemUsage
			i.IoRead = m.IoRead
			i.IoWrite = m.IoWrite
		}

		val := i
		instMap[i.SystemID] = append(instMap[i.SystemID], &val)
	}
	return instMap
}

// CleanCacheForSystem 清理指定系统的缓存 (供 DeleteSystem 使用)
func (im *InstanceManager) CleanCacheForSystem(systemID string) {
	rows, _ := im.db.Query("SELECT id FROM instance_infos WHERE system_id = ?", systemID)
	defer rows.Close()
	for rows.Next() {
		var id string
		rows.Scan(&id)
		im.metricsCache.Delete(id)
	}
}
