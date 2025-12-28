package manager

import (
	"database/sql"
	"log"
	"sync"
	"time"

	"ops-system/internal/master/monitor"
	"ops-system/pkg/protocol"

	_ "modernc.org/sqlite"
)

type NodeManager struct {
	db               *sql.DB
	tsdb             *monitor.MemoryTSDB
	offlineThreshold time.Duration

	// 【核心优化】全量节点内存缓存
	// Key: IP (string), Value: protocol.NodeInfo
	nodeCache sync.Map
}

func NewNodeManager(db *sql.DB, tsdb *monitor.MemoryTSDB, offlineThreshold time.Duration) *NodeManager {
	if offlineThreshold <= 0 {
		offlineThreshold = 30 * time.Second
	}

	nm := &NodeManager{
		db:               db,
		tsdb:             tsdb,
		offlineThreshold: offlineThreshold,
	}

	// 1. 启动时将 DB 数据加载到内存
	nm.loadFromDB()

	// 2. 启动后台同步协程 (可选，用于定期将最新的 Heartbeat 时间刷回 DB，防止重启后状态回退太多)
	go nm.syncLoop()

	return nm
}

// loadFromDB 从数据库预热缓存
func (nm *NodeManager) loadFromDB() {
	query := `
		SELECT 
			ip, port, hostname, name, COALESCE(mac_addr, ''), os, COALESCE(arch, ''), 
			COALESCE(cpu_cores, 0), COALESCE(mem_total, 0), COALESCE(disk_total, 0),
			status, last_heartbeat
		FROM node_infos
	`
	rows, err := nm.db.Query(query)
	if err != nil {
		log.Printf("[NodeManager] Load DB failed: %v", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var n protocol.NodeInfo
		rows.Scan(
			&n.IP, &n.Port, &n.Hostname, &n.Name, &n.MacAddr, &n.OS, &n.Arch,
			&n.CPUCores, &n.MemTotal, &n.DiskTotal,
			&n.Status, &n.LastHeartbeat,
		)
		// 存入缓存
		nm.nodeCache.Store(n.IP, n)
		count++
	}
	log.Printf("[NodeManager] Loaded %d nodes into memory cache", count)
}

// syncLoop 定期将内存状态刷回 DB (低频写入，例如 1分钟一次)
func (nm *NodeManager) syncLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		nm.persistDirtyNodes()
	}
}

func (nm *NodeManager) persistDirtyNodes() {
	// 简单策略：遍历内存，更新所有 Online 节点的心跳时间到 DB
	// 这是一个批量更新的过程，比单次请求更新要高效得多
	tx, err := nm.db.Begin()
	if err != nil {
		return
	}

	nm.nodeCache.Range(func(key, value interface{}) bool {
		node := value.(protocol.NodeInfo)
		// 只更新在线节点，减少 IO
		if node.Status == "online" {
			// 只更新心跳和状态，静态信息一般不变
			_, err := tx.Exec("UPDATE node_infos SET last_heartbeat = ?, status = ? WHERE ip = ?", node.LastHeartbeat, node.Status, node.IP)
			if err != nil {
				log.Printf("[Sync] Update failed: %v", err)
			}
		}
		return true
	})
	tx.Commit()
}

// HandleHeartbeat 处理心跳 (纯内存操作，极速)
func (nm *NodeManager) HandleHeartbeat(req protocol.RegisterRequest, remoteIP string) {
	now := time.Now().Unix()

	// 1. 写入时序库 (监控图表)
	if nm.tsdb != nil {
		nm.tsdb.Write(remoteIP, "node_cpu_usage", req.Status.CPUUsage)
		nm.tsdb.Write(remoteIP, "node_mem_usage", req.Status.MemUsage)
		nm.tsdb.Write(remoteIP, "node_net_in", req.Status.NetInSpeed)
		nm.tsdb.Write(remoteIP, "node_net_out", req.Status.NetOutSpeed)
	}

	// 2. 检查缓存中是否存在
	val, exists := nm.nodeCache.Load(remoteIP)

	if exists {
		// --- 场景 A: 老节点 (只更新内存) ---
		node := val.(protocol.NodeInfo)

		// 更新动态数据
		node.LastHeartbeat = now
		node.Status = "online"
		node.Port = req.Port // 端口可能会变

		// 更新监控快照 (用于列表显示)
		node.CPUUsage = req.Status.CPUUsage
		node.MemUsage = req.Status.MemUsage
		node.NetInSpeed = req.Status.NetInSpeed
		node.NetOutSpeed = req.Status.NetOutSpeed

		// *可选*: 如果 Hostname 变了，可能需要触发一次 DB 更新，这里暂略

		// 写回缓存
		nm.nodeCache.Store(remoteIP, node)

	} else {
		// --- 场景 B: 新节点 (写入 DB + 内存) ---
		log.Printf(">>> [NodeManager] New Node Detected: %s", remoteIP)

		newNode := protocol.NodeInfo{
			IP: remoteIP, Port: req.Port, Hostname: req.Info.Hostname, Name: req.Info.Hostname, // 默认名
			MacAddr: req.Info.MacAddr, OS: req.Info.OS, Arch: req.Info.Arch,
			CPUCores: req.Info.CPUCores, MemTotal: req.Info.MemTotal, DiskTotal: req.Info.DiskTotal,
			Status: "online", LastHeartbeat: now,
			// 监控数据
			CPUUsage: req.Status.CPUUsage, MemUsage: req.Status.MemUsage,
		}

		// 1. 存 DB (同步写入，保证持久化)
		insertSQL := `INSERT OR REPLACE INTO node_infos (
			ip, port, hostname, name, mac_addr, os, arch, 
			cpu_cores, mem_total, disk_total, status, last_heartbeat
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		_, err := nm.db.Exec(insertSQL,
			newNode.IP, newNode.Port, newNode.Hostname, newNode.Name, newNode.MacAddr, newNode.OS, newNode.Arch,
			newNode.CPUCores, newNode.MemTotal, newNode.DiskTotal, "online", now,
		)

		if err != nil {
			log.Printf("!!! [NodeManager] DB Insert Failed: %v", err)
			return // DB 失败暂不更新缓存，等待下一次重试
		}

		// 2. 存缓存
		nm.nodeCache.Store(remoteIP, newNode)
	}
}

// GetAllNodes 获取列表 (直接读内存，极速)
func (nm *NodeManager) GetAllNodes() []protocol.NodeInfo {
	var nodes []protocol.NodeInfo
	now := time.Now().Unix()
	threshold := int64(nm.offlineThreshold.Seconds())

	nm.nodeCache.Range(func(key, value interface{}) bool {
		n := value.(protocol.NodeInfo)

		// 动态离线判定 (只在读的时候计算，不修改存储)
		if n.Status == "online" && (now-n.LastHeartbeat > threshold) {
			n.Status = "offline"
			n.CPUUsage = 0
			n.MemUsage = 0
		}

		nodes = append(nodes, n)
		return true
	})

	return nodes
}

// GetNode 获取单个节点 (优先读缓存)
func (nm *NodeManager) GetNode(ip string) (*protocol.NodeInfo, bool) {
	if val, ok := nm.nodeCache.Load(ip); ok {
		n := val.(protocol.NodeInfo)
		return &n, true
	}
	// 如果缓存里没有，理论上 DB 里也没有，或者启动时加载漏了。
	// 这里可以做一个兜底查 DB，但通常不需要。
	return nil, false
}

// GetAllNodesMetrics 获取监控快照
func (nm *NodeManager) GetAllNodesMetrics() map[string]protocol.NodeInfo {
	res := make(map[string]protocol.NodeInfo)
	nm.nodeCache.Range(func(key, value interface{}) bool {
		n := value.(protocol.NodeInfo)
		res[n.IP] = n
		return true
	})
	return res
}

// --- 以下 CRUD 操作需要同时更新 DB 和 缓存 ---

func (nm *NodeManager) AddPlannedNode(ip, name string) error {
	// 1. 写 DB
	_, err := nm.db.Exec(`INSERT INTO node_infos (ip, port, name, status, hostname, last_heartbeat) VALUES (?, 0, ?, 'planned', '待接入', 0)`, ip, name)
	if err != nil {
		return err
	}

	// 2. 写缓存
	nm.nodeCache.Store(ip, protocol.NodeInfo{
		IP: ip, Name: name, Status: "planned", Hostname: "待接入",
	})
	return nil
}

func (nm *NodeManager) DeleteNode(ip string) error {
	_, err := nm.db.Exec("DELETE FROM node_infos WHERE ip = ?", ip)
	if err != nil {
		return err
	}
	nm.nodeCache.Delete(ip)
	return nil
}

func (nm *NodeManager) RenameNode(ip, newName string) error {
	_, err := nm.db.Exec("UPDATE node_infos SET name = ? WHERE ip = ?", newName, ip)
	if err != nil {
		return err
	}

	// 更新缓存
	if val, ok := nm.nodeCache.Load(ip); ok {
		n := val.(protocol.NodeInfo)
		n.Name = newName
		nm.nodeCache.Store(ip, n)
	}
	return nil
}

func (nm *NodeManager) ResetNodeName(ip string) error {
	_, err := nm.db.Exec("UPDATE node_infos SET name = hostname WHERE ip = ?", ip)
	if err != nil {
		return err
	}

	if val, ok := nm.nodeCache.Load(ip); ok {
		n := val.(protocol.NodeInfo)
		n.Name = n.Hostname
		nm.nodeCache.Store(ip, n)
	}
	return nil
}

func (nm *NodeManager) SetOfflineThreshold(d time.Duration) {
	// 只需要更新内存变量，无需加锁，因为 int64/duration 赋值原子性较高，且有点误差不影响
	if d > 0 {
		nm.offlineThreshold = d
	}
}
