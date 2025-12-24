package manager_test

import (
	"database/sql"
	"testing"

	"ops-system/internal/master/manager"
	"ops-system/pkg/protocol"

	"github.com/stretchr/testify/assert"
	_ "modernc.org/sqlite" // 注册驱动
)

// setupTestDB 初始化内存数据库并建表
func setupTestDB(t *testing.T) *sql.DB {
	// 使用内存模式，每个连接都是独立的，所以测试函数间不会干扰
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("Failed to open db: %v", err)
	}

	// 手动初始化表结构 (复制自 db/sqlite.go，或者如果 db 包有导出 InitTables 可复用)
	sqls := []string{
		`CREATE TABLE IF NOT EXISTS system_infos (id TEXT PRIMARY KEY, name TEXT, description TEXT, create_time INTEGER);`,
		`CREATE TABLE IF NOT EXISTS system_modules (id TEXT PRIMARY KEY, system_id TEXT, module_name TEXT, package_name TEXT, package_version TEXT, description TEXT);`,
		`CREATE TABLE IF NOT EXISTS instance_infos (id TEXT PRIMARY KEY, system_id TEXT, node_ip TEXT, service_name TEXT, service_version TEXT, status TEXT, pid INTEGER, uptime INTEGER);`,
	}

	for _, sqlStmt := range sqls {
		_, err := db.Exec(sqlStmt)
		assert.NoError(t, err)
	}

	return db
}

func TestSystemLifecycle(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()

	sysMgr := manager.NewSystemManager(db)
	instMgr := manager.NewInstanceManager(db)

	// 1. 测试创建系统
	sys := sysMgr.CreateSystem("PaymentSys", "Core Payment")
	assert.NotEmpty(t, sys.ID)
	assert.Equal(t, "PaymentSys", sys.Name)

	// 验证数据库
	var count int
	err := db.QueryRow("SELECT count(*) FROM system_infos").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)

	// 2. 测试添加模块
	err = sysMgr.AddModule(sys.ID, "API-Gateway", "gateway-pkg", "v1.0", "Main entry")
	assert.NoError(t, err)

	// 验证模块入库
	err = db.QueryRow("SELECT count(*) FROM system_modules WHERE system_id = ?", sys.ID).Scan(&count)
	assert.Equal(t, 1, count)

	// 3. 模拟添加一个实例 (为了测试级联删除)
	instMgr.RegisterInstance(&protocol.InstanceInfo{
		ID:          "inst-001",
		SystemID:    sys.ID,
		NodeIP:      "127.0.0.1",
		ServiceName: "gateway-pkg",
		Status:      "running",
	})

	// 4. 测试 GetFullView 聚合
	// GetFullView 返回 interface{}，需要断言转换
	viewInterface := sysMgr.GetFullView(instMgr)
	views, ok := viewInterface.([]protocol.SystemView)
	assert.True(t, ok)
	assert.Equal(t, 1, len(views))
	assert.Equal(t, "PaymentSys", views[0].Name)
	assert.Equal(t, 1, len(views[0].Modules))
	assert.Equal(t, "API-Gateway", views[0].Modules[0].ModuleName)
	assert.Equal(t, 1, len(views[0].Instances))
	assert.Equal(t, "inst-001", views[0].Instances[0].ID)

	// 5. 测试级联删除
	// 删除系统，应该同时删除 Module 和 Instance
	err = sysMgr.DeleteSystem(sys.ID, instMgr)
	assert.NoError(t, err)

	// 验证全部清空
	db.QueryRow("SELECT count(*) FROM system_infos").Scan(&count)
	assert.Equal(t, 0, count)
	db.QueryRow("SELECT count(*) FROM system_modules").Scan(&count)
	assert.Equal(t, 0, count)
	db.QueryRow("SELECT count(*) FROM instance_infos").Scan(&count)
	assert.Equal(t, 0, count)
}

func TestDeleteModule(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	sysMgr := manager.NewSystemManager(db)

	sys := sysMgr.CreateSystem("Test", "")
	sysMgr.AddModule(sys.ID, "Mod1", "Pkg", "v1", "")

	// 获取刚插入的 Module ID
	var modID string
	db.QueryRow("SELECT id FROM system_modules").Scan(&modID)

	// 删除
	err := sysMgr.DeleteModule(modID)
	assert.NoError(t, err)

	var count int
	db.QueryRow("SELECT count(*) FROM system_modules").Scan(&count)
	assert.Equal(t, 0, count)
}
