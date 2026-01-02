package manager

import (
	"database/sql"
	"fmt"
	"log"
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
	query := `INSERT OR REPLACE INTO instance_infos (id, system_id, node_id, service_name, service_version, status, pid, uptime) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	im.db.Exec(query, inst.ID, inst.SystemID, inst.NodeID, inst.ServiceName, inst.ServiceVersion, inst.Status, inst.PID, inst.Uptime)
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
	if im == nil || im.db == nil {
		return
	}

	im.mu.Lock()
	defer im.mu.Unlock()

	// 1. 尝试更新
	res, err := im.db.Exec(`UPDATE instance_infos SET status=?, pid=?, uptime=? WHERE id=?`,
		report.Status, report.PID, report.Uptime, report.InstanceID)

	if err != nil {
		return
	}

	rows, _ := res.RowsAffected()

	// 2. [自愈逻辑] 如果更新行数为 0，说明是幽灵实例
	if rows == 0 {
		if report.SystemID != "" && report.NodeID != "" {
			// =======================================================
			// [新增] 2.1 检查父级 System 是否存在，不存在则自动创建
			// =======================================================
			var sysCount int
			err := im.db.QueryRow("SELECT COUNT(1) FROM system_infos WHERE id = ?", report.SystemID).Scan(&sysCount)
			if err == nil && sysCount == 0 {
				// 自动恢复 System
				// 由于丢失了原始 Name，这里暂时用 ID 或 ServiceName 作为名称，用户后续可重命名
				recoverName := fmt.Sprintf("Recovered (%s)", report.ServiceName)
				log.Printf("[AutoDiscovery] Recovering missing System: %s", report.SystemID)

				im.db.Exec(`INSERT INTO system_infos (id, name, description, create_time) VALUES (?, ?, ?, ?)`,
					report.SystemID, recoverName, "Auto recovered from worker report", time.Now().Unix())
			}
			// =======================================================

			log.Printf("[AutoDiscovery] Registering ghost instance: %s", report.InstanceID)

			insertSQL := `INSERT INTO instance_infos (
				id, system_id, node_id, service_name, service_version, status, pid, uptime
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

			im.db.Exec(insertSQL,
				report.InstanceID,
				report.SystemID,
				report.NodeID,
				report.ServiceName,
				report.ServiceVersion,
				report.Status,
				report.PID,
				report.Uptime,
			)
		}
	}

	// 3. 更新内存 Metrics
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
	err := im.db.QueryRow(`SELECT id, system_id, node_id, service_name, service_version, status, pid, uptime FROM instance_infos WHERE id = ?`, id).
		Scan(&inst.ID, &inst.SystemID, &inst.NodeID, &inst.ServiceName, &inst.ServiceVersion, &inst.Status, &inst.PID, &inst.Uptime)
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
	// 修复点：将 node_ip 改为 node_id
	query := `SELECT id, system_id, node_id, service_name, service_version, status, pid, uptime FROM instance_infos WHERE system_id = ?`
	rows, err := im.db.Query(query, systemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var instances []protocol.InstanceInfo
	for rows.Next() {
		var i protocol.InstanceInfo
		err := rows.Scan(&i.ID, &i.SystemID, &i.NodeID, &i.ServiceName, &i.ServiceVersion, &i.Status, &i.PID, &i.Uptime)
		if err != nil {
			continue
		}

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
	// 修复点：将 node_ip 改为 node_id
	instRows, err := im.db.Query(`SELECT id, system_id, node_id, service_name, service_version, status, pid, uptime FROM instance_infos`)
	if err != nil {
		log.Printf("[Error] GetAllInstances query failed: %v", err)
		return make(map[string][]*protocol.InstanceInfo)
	}
	defer instRows.Close()

	instMap := make(map[string][]*protocol.InstanceInfo)
	for instRows.Next() {
		var i protocol.InstanceInfo
		// 修复点：对应 Scan node_id
		err := instRows.Scan(&i.ID, &i.SystemID, &i.NodeID, &i.ServiceName, &i.ServiceVersion, &i.Status, &i.PID, &i.Uptime)
		if err != nil {
			continue
		}

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

// GetAllInstancesMetrics 获取所有实例的监控快照
func (im *InstanceManager) GetAllInstancesMetrics() map[string]protocol.InstanceInfo {
	// 复用 GetAllInstances 逻辑
	instMap := im.GetAllInstances() // 返回的是 map[sysID][]*Info

	res := make(map[string]protocol.InstanceInfo)
	for _, list := range instMap {
		for _, inst := range list {
			// 如果是指针，取值
			res[inst.ID] = *inst
		}
	}
	return res
}

// GetAllInstanceIDs 获取所有登记在册的实例 ID (用于孤儿扫描白名单)
func (im *InstanceManager) GetAllInstanceIDs() ([]string, error) {
	rows, err := im.db.Query("SELECT id FROM instance_infos")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
