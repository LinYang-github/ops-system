package manager

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"ops-system/internal/master/monitor"
	"ops-system/pkg/protocol"

	_ "modernc.org/sqlite"
)

// 节点实时监控数据 (仅存内存)
type nodeMetrics struct {
	CPUUsage    float64
	MemUsage    float64
	NetInSpeed  float64
	NetOutSpeed float64
}

type NodeManager struct {
	db           *sql.DB
	mu           sync.Mutex
	metricsCache sync.Map // key: IP, value: nodeMetrics
	tsdb         *monitor.MemoryTSDB

	// 离线判定阈值
	offlineThreshold time.Duration
}

func NewNodeManager(db *sql.DB, tsdb *monitor.MemoryTSDB, offlineThreshold time.Duration) *NodeManager {
	// 防止阈值为 0 导致所有节点立即离线
	if offlineThreshold <= 0 {
		offlineThreshold = 30 * time.Second
	}

	return &NodeManager{
		db:               db,
		tsdb:             tsdb,
		offlineThreshold: offlineThreshold,
	}
}

// SetOfflineThreshold 动态设置离线阈值 (修复报错的关键方法)
func (nm *NodeManager) SetOfflineThreshold(d time.Duration) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	if d > 0 {
		nm.offlineThreshold = d
	}
}

// HandleHeartbeat 处理心跳 (读写分离版)
func (nm *NodeManager) HandleHeartbeat(req protocol.RegisterRequest, remoteIP string) {
	// 1. 动态数据 -> 存内存 (无锁/高频)
	metrics := nodeMetrics{
		CPUUsage:    req.Status.CPUUsage,
		MemUsage:    req.Status.MemUsage,
		NetInSpeed:  req.Status.NetInSpeed,
		NetOutSpeed: req.Status.NetOutSpeed,
	}
	nm.metricsCache.Store(remoteIP, metrics)

	// 写入时序数据库
	if nm.tsdb != nil {
		nm.tsdb.Write(remoteIP, "node_cpu_usage", req.Status.CPUUsage)
		nm.tsdb.Write(remoteIP, "node_mem_usage", req.Status.MemUsage)
		nm.tsdb.Write(remoteIP, "node_net_in", req.Status.NetInSpeed)
		nm.tsdb.Write(remoteIP, "node_net_out", req.Status.NetOutSpeed)
	}

	// 2. 静态/状态数据 -> 存 SQLite (有锁/低频)
	nm.mu.Lock()
	defer nm.mu.Unlock()

	now := time.Now().Unix()

	var existsName string
	err := nm.db.QueryRow("SELECT name FROM node_infos WHERE ip = ?", remoteIP).Scan(&existsName)

	if err == sql.ErrNoRows {
		// 新节点插入
		insertSQL := `INSERT INTO node_infos (
			ip, port, hostname, name, mac_addr, os, arch, 
			cpu_cores, mem_total, disk_total, status, last_heartbeat
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		name := req.Info.Hostname

		_, err := nm.db.Exec(insertSQL,
			remoteIP, req.Port, req.Info.Hostname, name, req.Info.MacAddr, req.Info.OS, req.Info.Arch,
			req.Info.CPUCores, req.Info.MemTotal, req.Info.DiskTotal, "online", now,
		)
		if err != nil {
			log.Printf("!!! [NodeManager] Insert Failed: %v", err)
		}
	} else {
		// 更新静态信息
		updateSQL := `UPDATE node_infos SET 
			port=?, hostname=?, mac_addr=?, os=?, status=?, last_heartbeat=?
			WHERE ip=?`

		_, err := nm.db.Exec(updateSQL,
			req.Port, req.Info.Hostname, req.Info.MacAddr, req.Info.OS, "online", now,
			remoteIP,
		)
		if err != nil {
			log.Printf("!!! [NodeManager] Update Failed: %v", err)
		}
	}
}

// GetAllNodes 获取列表 (合并 DB 和 内存)
func (nm *NodeManager) GetAllNodes() []protocol.NodeInfo {
	query := `
		SELECT 
			ip, port, hostname, name, COALESCE(mac_addr, ''), os, COALESCE(arch, ''), 
			COALESCE(cpu_cores, 0), COALESCE(mem_total, 0), COALESCE(disk_total, 0),
			status, last_heartbeat
		FROM node_infos
	`
	rows, err := nm.db.Query(query)
	if err != nil {
		log.Printf("!!! [NodeManager] Query Failed: %v", err)
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
			log.Printf("!!! [NodeManager] Scan Row Failed: %v", err)
			continue
		}

		// 动态离线判定
		if n.Status == "online" && (now-n.LastHeartbeat > int64(nm.offlineThreshold.Seconds())) {
			n.Status = "offline"
		}

		// 从内存 Cache 填充监控数据
		if n.Status == "online" {
			if val, ok := nm.metricsCache.Load(n.IP); ok {
				m := val.(nodeMetrics)
				n.CPUUsage = m.CPUUsage
				n.MemUsage = m.MemUsage
				n.NetInSpeed = m.NetInSpeed
				n.NetOutSpeed = m.NetOutSpeed
			}
		} else {
			n.CPUUsage = 0
			n.MemUsage = 0
			n.NetInSpeed = 0
			n.NetOutSpeed = 0
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

// GetAllNodesMetrics 获取监控快照 (供 AlertManager 使用)
func (nm *NodeManager) GetAllNodesMetrics() map[string]protocol.NodeInfo {
	nodes := nm.GetAllNodes()
	res := make(map[string]protocol.NodeInfo)
	for _, n := range nodes {
		res[n.IP] = n
	}
	return res
}

func (nm *NodeManager) AddPlannedNode(ip, name string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	var count int
	nm.db.QueryRow("SELECT count(*) FROM node_infos WHERE ip = ?", ip).Scan(&count)
	if count > 0 {
		return fmt.Errorf("node ip already exists")
	}
	_, err := nm.db.Exec(`INSERT INTO node_infos (ip, port, name, status, hostname, last_heartbeat) VALUES (?, 0, ?, 'planned', '待接入', 0)`, ip, name)
	return err
}

func (nm *NodeManager) DeleteNode(ip string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.db.Exec("DELETE FROM node_infos WHERE ip = ?", ip)
	nm.metricsCache.Delete(ip)
	return nil
}

func (nm *NodeManager) RenameNode(ip, newName string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	_, err := nm.db.Exec("UPDATE node_infos SET name = ? WHERE ip = ?", newName, ip)
	return err
}

func (nm *NodeManager) ResetNodeName(ip string) error {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	_, err := nm.db.Exec("UPDATE node_infos SET name = hostname WHERE ip = ?", ip)
	return err
}
