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

	// Key: NodeID (string), Value: protocol.NodeInfo
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
			id, ip, port, hostname, name, COALESCE(mac_addr, ''), os, COALESCE(arch, ''), 
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
			&n.ID, &n.IP, &n.Port, &n.Hostname, &n.Name, &n.MacAddr, &n.OS, &n.Arch,
			&n.CPUCores, &n.MemTotal, &n.DiskTotal,
			&n.Status, &n.LastHeartbeat,
		)
		// 存入缓存，使用 ID 作为 Key
		nm.nodeCache.Store(n.ID, n)
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
			// 只更新心跳和状态，静态信息一般不变，同时更新 IP (防止 Worker IP 变动)
			_, err := tx.Exec("UPDATE node_infos SET last_heartbeat = ?, status = ?, ip = ?, port = ? WHERE id = ?",
				node.LastHeartbeat, node.Status, node.IP, node.Port, node.ID)
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
	nodeID := req.Info.ID

	// 1. 容错校验：如果 Worker 没有上报 ID (旧版本或异常)，记录日志并拒绝处理
	if nodeID == "" {
		log.Printf("⚠️ [NodeManager] Worker %s missing NodeID, ignoring heartbeat.", remoteIP)
		return
	}

	// 2. 写入时序库 (使用 NodeID 作为 Key，保证 IP 变更后监控数据不丢失)
	if nm.tsdb != nil {
		nm.tsdb.Write(nodeID, "node_cpu_usage", req.Status.CPUUsage)
		nm.tsdb.Write(nodeID, "node_mem_usage", req.Status.MemUsage)
		nm.tsdb.Write(nodeID, "node_disk_usage", req.Status.DiskUsage)
		nm.tsdb.Write(nodeID, "node_net_in", req.Status.NetInSpeed)
		nm.tsdb.Write(nodeID, "node_net_out", req.Status.NetOutSpeed)
	}

	// 3. 检查缓存中是否存在 (通过 ID 查)
	val, exists := nm.nodeCache.Load(nodeID)

	if exists {
		// ==============================
		// 场景 A: 老节点 (只更新内存状态)
		// ==============================
		node := val.(protocol.NodeInfo)

		// 3.1 更新核心状态
		node.LastHeartbeat = now
		node.Status = "online"

		// 3.2 【关键】始终更新网络地址
		// 即使是老节点，IP 和端口也可能发生变化 (DHCP, 容器重启等)
		// Master 后续调用 Worker 接口时将使用这里最新的 IP
		node.IP = remoteIP
		node.Port = req.Port

		// 3.3 更新实时监控快照 (用于列表展示)
		node.CPUUsage = req.Status.CPUUsage
		node.MemUsage = req.Status.MemUsage
		node.NetInSpeed = req.Status.NetInSpeed
		node.NetOutSpeed = req.Status.NetOutSpeed

		// 3.4 可选：如果 Hostname 变了，更新一下 (防止改名后不生效)
		if req.Info.Hostname != "" {
			node.Hostname = req.Info.Hostname
		}

		// 3.5 写回缓存
		nm.nodeCache.Store(nodeID, node)

	} else {
		// ==============================
		// 场景 B: 新节点 (写入 DB + 内存)
		// ==============================
		log.Printf(">>> [NodeManager] New Node Registered: %s (IP: %s)", nodeID, remoteIP)

		// 构造新节点对象
		// 注意：Name 默认为 Hostname，后续用户可在前端修改 Name
		name := req.Info.Name
		if name == "" {
			name = req.Info.Hostname
		}

		newNode := protocol.NodeInfo{
			ID:            nodeID,
			IP:            remoteIP,
			Port:          req.Port,
			Hostname:      req.Info.Hostname,
			Name:          name,
			MacAddr:       req.Info.MacAddr,
			OS:            req.Info.OS,
			Arch:          req.Info.Arch,
			CPUCores:      req.Info.CPUCores,
			MemTotal:      req.Info.MemTotal,
			DiskTotal:     req.Info.DiskTotal,
			Status:        "online",
			LastHeartbeat: now,
			// 初始监控值
			CPUUsage:    req.Status.CPUUsage,
			MemUsage:    req.Status.MemUsage,
			NetInSpeed:  req.Status.NetInSpeed,
			NetOutSpeed: req.Status.NetOutSpeed,
		}

		// 4.1 存 DB (同步写入，保证持久化)
		// 使用 INSERT OR REPLACE 确保 ID 冲突时覆盖旧数据
		insertSQL := `INSERT OR REPLACE INTO node_infos (
			id, ip, port, hostname, name, mac_addr, os, arch, 
			cpu_cores, mem_total, disk_total, status, last_heartbeat
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

		_, err := nm.db.Exec(insertSQL,
			newNode.ID, newNode.IP, newNode.Port, newNode.Hostname, newNode.Name, newNode.MacAddr, newNode.OS, newNode.Arch,
			newNode.CPUCores, newNode.MemTotal, newNode.DiskTotal, "online", now,
		)

		if err != nil {
			log.Printf("!!! [NodeManager] DB Insert Failed: %v", err)
			// DB 失败不更新缓存，等待下一次心跳重试
			return
		}

		// 4.2 存缓存
		nm.nodeCache.Store(nodeID, newNode)
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
func (nm *NodeManager) GetNode(id string) (*protocol.NodeInfo, bool) {
	if val, ok := nm.nodeCache.Load(id); ok {
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

func (nm *NodeManager) AddPlannedNode(id, name string) error {
	// 1. 写 DB
	_, err := nm.db.Exec(`INSERT INTO node_infos (id, port, name, status, hostname, last_heartbeat) VALUES (?, 0, ?, 'planned', '待接入', 0)`, id, name)
	if err != nil {
		return err
	}

	// 2. 写缓存
	nm.nodeCache.Store(id, protocol.NodeInfo{
		ID: id, Name: name, Status: "planned", Hostname: "待接入",
	})
	return nil
}

func (nm *NodeManager) DeleteNode(id string) error {
	_, err := nm.db.Exec("DELETE FROM node_infos WHERE id = ?", id)
	if err != nil {
		return err
	}
	nm.nodeCache.Delete(id)
	return nil
}

func (nm *NodeManager) RenameNode(id, newName string) error {
	_, err := nm.db.Exec("UPDATE node_infos SET name = ? WHERE id = ?", newName, id)
	if err != nil {
		return err
	}

	// 更新缓存
	if val, ok := nm.nodeCache.Load(id); ok {
		n := val.(protocol.NodeInfo)
		n.Name = newName
		nm.nodeCache.Store(id, n)
	}
	return nil
}

func (nm *NodeManager) ResetNodeName(id string) error {
	_, err := nm.db.Exec("UPDATE node_infos SET name = hostname WHERE id = ?", id)
	if err != nil {
		return err
	}

	if val, ok := nm.nodeCache.Load(id); ok {
		n := val.(protocol.NodeInfo)
		n.Name = n.Hostname
		nm.nodeCache.Store(id, n)
	}
	return nil
}

func (nm *NodeManager) SetOfflineThreshold(d time.Duration) {
	// 只需要更新内存变量，无需加锁，因为 int64/duration 赋值原子性较高，且有点误差不影响
	if d > 0 {
		nm.offlineThreshold = d
	}
}
