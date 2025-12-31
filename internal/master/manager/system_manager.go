package manager

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"ops-system/pkg/protocol"
)

// SystemManager 只负责系统和模块元数据
type SystemManager struct {
	db *sql.DB
	mu sync.Mutex
}

func NewSystemManager(db *sql.DB) *SystemManager {
	return &SystemManager{db: db}
}

// CreateSystem 创建系统
func (sm *SystemManager) CreateSystem(name, desc string) *protocol.SystemInfo {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	id := fmt.Sprintf("sys-%d", time.Now().UnixNano())
	now := time.Now().Unix()
	sm.db.Exec(`INSERT INTO system_infos VALUES (?, ?, ?, ?)`, id, name, desc, now)
	return &protocol.SystemInfo{ID: id, Name: name, Description: desc, CreateTime: now}
}

// DeleteSystem 删除系统 (需要清理实例，所以需要传入 InstanceManager)
func (sm *SystemManager) DeleteSystem(systemID string, im *InstanceManager) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// 1. 清理缓存 (调用 InstanceManager)
	im.CleanCacheForSystem(systemID)

	// 2. 数据库级联删除
	tx, _ := sm.db.Begin()
	tx.Exec("DELETE FROM instance_infos WHERE system_id = ?", systemID)
	tx.Exec("DELETE FROM system_modules WHERE system_id = ?", systemID)
	tx.Exec("DELETE FROM system_infos WHERE id = ?", systemID)
	return tx.Commit()
}

// AddModule 添加模块
func (sm *SystemManager) AddModule(m protocol.SystemModule) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	id := fmt.Sprintf("mod-%d", time.Now().UnixNano())
	query := `INSERT INTO system_modules 
	(id, system_id, module_name, package_name, package_version, description, start_order, readiness_type, readiness_target, readiness_timeout) 
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := sm.db.Exec(query,
		id, m.SystemID, m.ModuleName, m.PackageName, m.PackageVersion, m.Description,
		m.StartOrder, m.ReadinessType, m.ReadinessTarget, m.ReadinessTimeout,
	)
	return err
}

// 顺便增加一个 GetModule 用于部署时查询配置
func (sm *SystemManager) GetModule(systemID, pkgName, pkgVer string) (*protocol.SystemModule, error) {
	var m protocol.SystemModule
	// 这里假设同一个系统下，同一个包名版本组合只有一个模块定义
	// 实际场景最好用 ModuleID 查，但目前 DeployInstance 传参没带 ModuleID
	// 暂时用 3 个字段匹配
	query := `SELECT id, system_id, module_name, package_name, package_version, description, start_order, readiness_type, readiness_target, readiness_timeout 
	          FROM system_modules 
			  WHERE system_id = ? AND package_name = ? AND package_version = ? LIMIT 1`

	err := sm.db.QueryRow(query, systemID, pkgName, pkgVer).Scan(
		&m.ID, &m.SystemID, &m.ModuleName, &m.PackageName, &m.PackageVersion, &m.Description,
		&m.StartOrder, &m.ReadinessType, &m.ReadinessTarget, &m.ReadinessTimeout,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// DeleteModule 删除模块
func (sm *SystemManager) DeleteModule(modID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	_, err := sm.db.Exec("DELETE FROM system_modules WHERE id = ?", modID)
	return err
}

// GetFullView 聚合视图 (需要 InstanceManager 提供实例数据)
func (sm *SystemManager) GetFullView(im *InstanceManager) interface{} {
	// 1. 获取所有系统
	sysRows, err := sm.db.Query(`SELECT id, name, description, create_time FROM system_infos ORDER BY create_time DESC`)
	if err != nil {
		return []protocol.SystemView{}
	}
	defer sysRows.Close()
	var systems []protocol.SystemInfo
	for sysRows.Next() {
		var s protocol.SystemInfo
		if err := sysRows.Scan(&s.ID, &s.Name, &s.Description, &s.CreateTime); err != nil {
			continue
		}
		systems = append(systems, s)
	}

	// 2. Modules (增加错误校验和字段顺序对齐)
	modRows, err := sm.db.Query(`SELECT id, system_id, module_name, package_name, package_version, description, start_order, readiness_type, readiness_target, readiness_timeout FROM system_modules`)
	if err != nil {
		log.Printf("[Error] Query system_modules failed: %v", err)
	} else {
		defer modRows.Close()
	}

	modMap := make(map[string][]*protocol.SystemModule)
	if modRows != nil {
		for modRows.Next() {
			var m protocol.SystemModule
			var rType, rTarget sql.NullString
			var rTimeout sql.NullInt64

			// 严格按 SELECT 顺序进行 Scan
			err := modRows.Scan(
				&m.ID, &m.SystemID, &m.ModuleName, &m.PackageName, &m.PackageVersion,
				&m.Description, &m.StartOrder, &rType, &rTarget, &rTimeout,
			)
			if err != nil {
				log.Printf("[Warn] Scan module failed: %v", err)
				continue
			}

			m.ReadinessType = rType.String
			m.ReadinessTarget = rTarget.String
			m.ReadinessTimeout = int(rTimeout.Int64)

			val := m
			modMap[m.SystemID] = append(modMap[m.SystemID], &val)
		}
	}

	// 3. 获取所有实例
	var instMap map[string][]*protocol.InstanceInfo
	if im != nil {
		instMap = im.GetAllInstances()
	} else {
		instMap = make(map[string][]*protocol.InstanceInfo)
	}

	// 4. 组装结果
	var result []protocol.SystemView
	for _, sys := range systems {
		view := protocol.SystemView{
			SystemInfo: &sys,
			Modules:    modMap[sys.ID],
			Instances:  instMap[sys.ID],
		}
		if view.Modules == nil {
			view.Modules = []*protocol.SystemModule{}
		}
		if view.Instances == nil {
			view.Instances = []*protocol.InstanceInfo{}
		}
		result = append(result, view)
	}
	return result
}

// GetAllSystemIDs 获取所有合法的系统 ID (作为目录白名单)
func (sm *SystemManager) GetAllSystemIDs() ([]string, error) {
	// 改为查询 id 字段
	rows, err := sm.db.Query("SELECT id FROM system_infos")
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
