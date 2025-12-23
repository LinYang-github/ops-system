package manager

import (
	"database/sql"
	"fmt"
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
func (sm *SystemManager) AddModule(sysID, name, pkgName, pkgVer, desc string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	id := fmt.Sprintf("mod-%d", time.Now().UnixNano())
	_, err := sm.db.Exec(`INSERT INTO system_modules VALUES (?, ?, ?, ?, ?, ?)`, id, sysID, name, pkgName, pkgVer, desc)
	return err
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
	sysRows, _ := sm.db.Query(`SELECT id, name, description, create_time FROM system_infos ORDER BY create_time DESC`)
	defer sysRows.Close()
	var systems []protocol.SystemInfo
	for sysRows.Next() {
		var s protocol.SystemInfo
		sysRows.Scan(&s.ID, &s.Name, &s.Description, &s.CreateTime)
		systems = append(systems, s)
	}

	// 2. 获取所有模块
	modRows, _ := sm.db.Query(`SELECT id, system_id, module_name, package_name, package_version, description FROM system_modules`)
	defer modRows.Close()
	modMap := make(map[string][]*protocol.SystemModule)
	for modRows.Next() {
		var m protocol.SystemModule
		modRows.Scan(&m.ID, &m.SystemID, &m.ModuleName, &m.PackageName, &m.PackageVersion, &m.Description)
		val := m
		modMap[m.SystemID] = append(modMap[m.SystemID], &val)
	}

	// 3. 获取所有实例 (调用 InstanceManager)
	instMap := im.GetAllInstances()

	// 4. 组装
	var result []protocol.SystemView
	for i := range systems {
		sys := systems[i]
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
