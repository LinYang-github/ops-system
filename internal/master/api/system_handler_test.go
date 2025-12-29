package api_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ops-system/internal/master/api"
	"ops-system/internal/master/manager"
	"ops-system/internal/master/ws"
	"ops-system/pkg/protocol"

	"github.com/stretchr/testify/assert"
	_ "modernc.org/sqlite"
)

// 辅助函数：初始化完整的 Handler 环境
func setupHandler(t *testing.T) (*api.ServerHandler, *sql.DB) {
	// 1. 初始化 DB
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("db error: %v", err)
	}
	// 建表
	sqls := []string{
		`CREATE TABLE IF NOT EXISTS system_infos (id TEXT PRIMARY KEY, name TEXT, description TEXT, create_time INTEGER);`,
		`CREATE TABLE IF NOT EXISTS system_modules (id TEXT PRIMARY KEY, system_id TEXT, module_name TEXT, package_name TEXT, package_version TEXT, description TEXT);`,
		`CREATE TABLE IF NOT EXISTS instance_infos (id TEXT PRIMARY KEY, system_id TEXT, node_ip TEXT, service_name TEXT, service_version TEXT, status TEXT, pid INTEGER, uptime INTEGER);`,
		`CREATE TABLE IF NOT EXISTS sys_op_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, operator TEXT, action TEXT, target_type TEXT, target_name TEXT, detail TEXT, status TEXT, create_time INTEGER);`,
	}
	for _, s := range sqls {
		db.Exec(s)
	}

	// 2. 初始化 Managers
	// 注意：这里需要传入 nil 的 monitorStore 等，因为我们只测 System Handler
	sysMgr := manager.NewSystemManager(db)
	instMgr := manager.NewInstanceManager(db)
	logMgr := manager.NewLogManager(db)
	// 其他 manager 可以是 nil，只要 SystemHandler 不用到它们

	// 3. 启动 WS Hub (防止 broadcast 阻塞)
	go ws.GlobalHub.Run()

	// 4. 构造 Handler
	h := api.NewServerHandler(sysMgr, instMgr, nil, logMgr, nil, nil, nil, nil, nil, nil)
	return h, db
}

func TestHandleCreateSystem(t *testing.T) {
	h, db := setupHandler(t)
	defer db.Close()

	// 构造请求
	reqBody := `{"name":"HandlerTest", "description":"Via API"}`
	req := httptest.NewRequest("POST", "/api/systems/create", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	// 调用方法
	h.CreateSystem(w, req)

	// 断言 HTTP 状态
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 断言响应 Body
	var sys protocol.SystemInfo
	err := json.NewDecoder(resp.Body).Decode(&sys)
	assert.NoError(t, err)
	assert.Equal(t, "HandlerTest", sys.Name)
	assert.NotEmpty(t, sys.ID)

	// 断言 DB 副作用 (同步操作，直接查)
	var count int
	db.QueryRow("SELECT count(*) FROM system_infos").Scan(&count)
	assert.Equal(t, 1, count)

	// 【修改点】断言日志记录 (异步操作，使用 Eventually)
	// 含义：在 1秒内，每 10毫秒检查一次，直到返回 true
	assert.Eventually(t, func() bool {
		var logCount int
		db.QueryRow("SELECT count(*) FROM sys_op_logs WHERE action='create_system'").Scan(&logCount)
		return logCount == 1
	}, 1*time.Second, 10*time.Millisecond, "Log should be recorded asynchronously")
}

func TestHandleGetSystems(t *testing.T) {
	h, db := setupHandler(t)
	defer db.Close()

	// 先往库里插一条数据
	db.Exec(`INSERT INTO system_infos (id, name) VALUES ('sys-1', 'Preload')`)

	req := httptest.NewRequest("GET", "/api/systems", nil)
	w := httptest.NewRecorder()

	h.GetSystems(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var views []protocol.SystemView
	json.NewDecoder(w.Body).Decode(&views)

	assert.Len(t, views, 1)
	assert.Equal(t, "Preload", views[0].Name)
}

func TestHandleCreateSystemModule(t *testing.T) {
	h, db := setupHandler(t)
	defer db.Close()

	// 先建系统
	db.Exec(`INSERT INTO system_infos (id, name) VALUES ('sys-1', 'Base')`)

	// 调接口加模块
	reqBody := `{"system_id":"sys-1", "module_name":"API", "package_name":"pkg", "package_version":"v1"}`
	req := httptest.NewRequest("POST", "/api/systems/module/add", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	h.CreateSystemModule(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// 查库验证
	var modName string
	err := db.QueryRow("SELECT module_name FROM system_modules WHERE system_id='sys-1'").Scan(&modName)
	assert.NoError(t, err)
	assert.Equal(t, "API", modName)
}

func TestHandleDeleteSystem(t *testing.T) {
	h, db := setupHandler(t)
	defer db.Close()

	db.Exec(`INSERT INTO system_infos (id, name) VALUES ('sys-del', 'ToDel')`)

	reqBody := `{"id":"sys-del"}`
	req := httptest.NewRequest("POST", "/api/systems/delete", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	h.DeleteSystem(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var count int
	db.QueryRow("SELECT count(*) FROM system_infos").Scan(&count)
	assert.Equal(t, 0, count)
}
