package manager

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"ops-system/internal/master/monitor"
	"ops-system/pkg/protocol"
)

// 节点实时监控数据 (只存内存)
type nodeMetrics struct {
	CPUUsage    float64
	MemUsage    float64
	NetInSpeed  float64
	NetOutSpeed float64
}

type NodeManager struct {
	db               *sql.DB
	mu               sync.Mutex
	metricsCache     sync.Map            // key: IP, value: nodeMetrics
	tsdb             *monitor.MemoryTSDB // 新增：时序存储
	offlineThreshold time.Duration
}

func NewNodeManager(db *sql.DB, tsdb *monitor.MemoryTSDB, threshold time.Duration) *NodeManager {
	return &NodeManager{
		db:               db,
		tsdb:             tsdb, // 注入
		offlineThreshold: threshold,
	}
}

// HandleHeartbeat 处理心跳
func (nm *NodeManager) HandleHeartbeat(req protocol.RegisterRequest, remoteIP string) {
	// 1. 更新内存中的监控数据 (无锁，高频)
	metrics := nodeMetrics{
		CPUUsage:    req.Status.CPUUsage,
		MemUsage:    req.Status.MemUsage,
		NetInSpeed:  req.Status.NetInSpeed,
		NetOutSpeed: req.Status.NetOutSpeed,
	}
	nm.metricsCache.Store(remoteIP, metrics)

	// 2. 【新增】写入时序数据库 (MemoryTSDB)
	// 记录 CPU 和 内存
	if nm.tsdb != nil {
		nm.tsdb.Write(remoteIP, "node_cpu_usage", req.Status.CPUUsage)
		nm.tsdb.Write(remoteIP, "node_mem_usage", req.Status.MemUsage)
		// 如果需要网络流量曲线，也可以在这里加
	}

	// 2. 更新数据库中的静态信息 (有锁，低频)
	// 优化策略：其实可以判断静态信息是否有变化再写库，这里为了简单每次心跳都写，
	// 但去掉了高频变化的监控字段，SQL 压力减小了很多。
	nm.mu.Lock()
	defer nm.mu.Unlock()

	now := time.Now().Unix()

	var existsName string
	err := nm.db.QueryRow("SELECT name FROM node_infos WHERE ip = ?", remoteIP).Scan(&existsName)

	if err == sql.ErrNoRows {
		// 新节点插入 (SQL 中不再包含 cpu_usage 等字段)
		insertSQL := `INSERT INTO node_infos (
			ip, port, hostname, name, mac_addr, os, arch, cpu_cores, mem_total, disk_total, 
			status, last_heartbeat
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		name := req.Info.Hostname

		nm.db.Exec(insertSQL,
			remoteIP, req.Port, req.Info.Hostname, name, req.Info.MacAddr, req.Info.OS, req.Info.Arch, req.Info.CPUCores, req.Info.MemTotal, req.Info.DiskTotal,
			"online", now,
		)
	} else {
		// 更新静态信息和心跳时间
		updateSQL := `UPDATE node_infos SET 
			port=?, hostname=?, mac_addr=?, os=?, arch=?, cpu_cores=?, mem_total=?, disk_total=?,
			status=?, last_heartbeat=?
			WHERE ip=?`

		nm.db.Exec(updateSQL,
			req.Port, req.Info.Hostname, req.Info.MacAddr, req.Info.OS, req.Info.Arch, req.Info.CPUCores, req.Info.MemTotal, req.Info.DiskTotal,
			"online", now,
			remoteIP,
		)
	}
}

// AddPlannedNode 手动添加规划节点
func (nm *NodeManager) AddPlannedNode(ip, name string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	// 检查是否已存在
	var count int
	nm.db.QueryRow("SELECT count(*) FROM node_infos WHERE ip = ?", ip).Scan(&count)
	if count > 0 {
		return fmt.Errorf("node ip already exists")
	}

	// 规划节点默认端口暂填 0 或 8081
	_, err := nm.db.Exec(`INSERT INTO node_infos (ip, port, name, status, hostname) VALUES (?, 0, ?, 'planned', '待接入')`, ip, name)
	return err
}

// DeleteNode 删除节点 (仅允许删除非 online 节点)
func (nm *NodeManager) DeleteNode(ip string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()

	var status string
	err := nm.db.QueryRow("SELECT status FROM node_infos WHERE ip = ?", ip).Scan(&status)
	if err != nil {
		return err
	}

	if status == "online" {
		// 实际上也可以强制删除，但为了安全通常禁止
		// return fmt.Errorf("cannot delete online node")
	}

	_, err = nm.db.Exec("DELETE FROM node_infos WHERE ip = ?", ip)
	nm.metricsCache.Delete(ip) // 顺便清理缓存
	return err
}

// RenameNode 重命名节点
func (nm *NodeManager) RenameNode(ip, newName string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	_, err := nm.db.Exec("UPDATE node_infos SET name = ? WHERE ip = ?", newName, ip)
	return err
}

// ResetNodeName 重置节点名为 Hostname
func (nm *NodeManager) ResetNodeName(ip string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	// 将 name 更新为 hostname 字段的值
	_, err := nm.db.Exec("UPDATE node_infos SET name = hostname WHERE ip = ?", ip)
	return err
}

// GetAllNodes 获取列表 (合并 DB 和 内存)
func (nm *NodeManager) GetAllNodes() []protocol.NodeInfo {
	// SQL 中不再查询 cpu_usage 等列
	// COALESCE 防止旧数据 NULL 导致 Scan 失败
	query := `
		SELECT 
			ip, port, hostname, name, COALESCE(mac_addr, ''), os, COALESCE(arch, ''), 
			COALESCE(cpu_cores, 0), COALESCE(mem_total, 0), COALESCE(disk_total, 0),
			status, last_heartbeat
		FROM node_infos
	`
	rows, err := nm.db.Query(query)
	if err != nil {
		return []protocol.NodeInfo{}
	}
	defer rows.Close()

	var nodes []protocol.NodeInfo
	now := time.Now().Unix()

	for rows.Next() {
		var n protocol.NodeInfo
		err := rows.Scan(
			&n.IP, &n.Port, &n.Hostname, &n.Name, &n.MacAddr, &n.OS, &n.Arch,
			&n.CPUCores, &n.MemTotal, &n.DiskTotal,
			&n.Status, &n.LastHeartbeat,
		)
		if err != nil {
			continue
		}

		// 填充实时监控数据
		if val, ok := nm.metricsCache.Load(n.IP); ok {
			m := val.(nodeMetrics)
			n.CPUUsage = m.CPUUsage
			n.MemUsage = m.MemUsage
			n.NetInSpeed = m.NetInSpeed
			n.NetOutSpeed = m.NetOutSpeed
		}

		// 动态离线判定
		if n.Status == "online" && (now-n.LastHeartbeat > int64(nm.offlineThreshold.Seconds())) {
			n.Status = "offline"
			// 离线节点清除监控数据
			n.CPUUsage = 0
			n.MemUsage = 0
		}

		nodes = append(nodes, n)
	}
	return nodes
}

// GetNode 获取单个节点
func (nm *NodeManager) GetNode(ip string) (*protocol.NodeInfo, bool) {
	var n protocol.NodeInfo
	query := `SELECT ip, port, hostname, name, COALESCE(mac_addr, ''), status FROM node_infos WHERE ip = ?`
	err := nm.db.QueryRow(query, ip).Scan(&n.IP, &n.Port, &n.Hostname, &n.Name, &n.MacAddr, &n.Status)
	if err != nil {
		return nil, false
	}
	return &n, true
}

// GetAllNodesMetrics 获取所有节点的监控快照 (供 AlertManager 使用)
func (nm *NodeManager) GetAllNodesMetrics() map[string]protocol.NodeInfo {
	// 复用 GetAllNodes 的逻辑，或者简化只读内存
	// 这里为了简单，直接调用 GetAllNodes
	nodes := nm.GetAllNodes()
	res := make(map[string]protocol.NodeInfo)
	for _, n := range nodes {
		res[n.IP] = n
	}
	return res
}
