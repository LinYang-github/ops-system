package api

import (
	"io/fs"
	"log"
	"net/http"

	"ops-system/internal/master/db"
	"ops-system/internal/master/manager"
	"ops-system/internal/master/monitor"
	"ops-system/internal/master/ws"
)

// 全局 Manager 实例 (供同一包下的 handlers 使用)
var (
	sysManager    *manager.SystemManager
	instManager   *manager.InstanceManager
	logManager    *manager.LogManager
	pkgManager    *manager.PackageManager
	nodeManager   *manager.NodeManager
	configManager *manager.ConfigManager
	backupManager *manager.BackupManager
	monitorStore  *monitor.MemoryTSDB
)

var uploadPath string

// StartMasterServer 启动 Master HTTP 服务
func StartMasterServer(port, dir, dbPath string, assets fs.FS) error {
	uploadPath = dir

	// 1. 初始化数据库 (传入路径)
	database := db.InitDB(dbPath)

	// 1. 初始化 Monitor Store
	monitorStore = monitor.NewMemoryTSDB()

	// 2. 初始化 Managers (依赖注入)
	logManager = manager.NewLogManager(database)
	sysManager = manager.NewSystemManager(database)
	instManager = manager.NewInstanceManager(database)
	nodeManager = manager.NewNodeManager(database, monitorStore)
	pkgManager = manager.NewPackageManager(uploadPath)
	configManager = manager.NewConfigManager(database)
	backupManager = manager.NewBackupManager(database, uploadPath)
	nodeManager = manager.NewNodeManager(database, monitorStore)
	// 3. 启动 WebSocket Hub
	go ws.GlobalHub.Run()

	// 4. 创建路由器
	mux := http.NewServeMux()
	registerRoutes(mux, assets)

	log.Printf("Master UI & API running on %s", port)

	server := &http.Server{
		Addr:         port,
		Handler:      mux,
		ReadTimeout:  0, // 支持大文件上传
		WriteTimeout: 0,
	}

	return server.ListenAndServe()
}

// 注册所有路由
func registerRoutes(mux *http.ServeMux, assets fs.FS) {
	// --- Node 相关 (node_handler.go) ---
	mux.HandleFunc("/api/worker/heartbeat", handleHeartbeat)
	mux.HandleFunc("/api/nodes", handleListNodes)
	mux.HandleFunc("/api/nodes/add", handleAddNode)
	mux.HandleFunc("/api/nodes/delete", handleDeleteNode)
	mux.HandleFunc("/api/nodes/rename", handleRenameNode)
	mux.HandleFunc("/api/nodes/reset_name", handleResetNodeName)
	mux.HandleFunc("/api/ctrl/cmd", handleTriggerCmd)

	// --- System 配置相关 (system_handler.go) ---
	mux.HandleFunc("/api/systems", handleGetSystems)
	mux.HandleFunc("/api/systems/create", handleCreateSystem)
	mux.HandleFunc("/api/systems/delete", handleDeleteSystem)
	mux.HandleFunc("/api/systems/module/add", handleCreateSystemModule)
	mux.HandleFunc("/api/systems/module/delete", handleDeleteSystemModule)

	// --- Instance 运行相关 (instance_handler.go) ---
	// 注意：这些函数从 system_handler.go 移到了 instance_handler.go
	mux.HandleFunc("/api/deploy", handleDeployInstance)
	mux.HandleFunc("/api/deploy/external", handleRegisterExternal)
	mux.HandleFunc("/api/instance/action", handleInstanceAction)
	mux.HandleFunc("/api/instance/status_report", handleWorkerInstanceStatusReport)
	mux.HandleFunc("/api/systems/action", handleSystemAction) // 批量操作

	// --- Package 相关 (package_handler.go) ---
	mux.HandleFunc("/api/upload", handleUploadPackage)
	mux.HandleFunc("/api/packages", handleListPackages)
	mux.HandleFunc("/api/packages/delete", handleDeletePackage)
	mux.HandleFunc("/api/packages/manifest", handleGetPackageManifest)

	// --- Log 相关 (log_handler.go) ---
	mux.HandleFunc("/api/logs", handleGetOpLogs)

	mux.HandleFunc("/api/nacos/settings", handleNacosSettings)
	mux.HandleFunc("/api/nacos/namespaces", handleNacosNamespaces)
	mux.HandleFunc("/api/nacos/configs", handleNacosConfigs)
	mux.HandleFunc("/api/nacos/config/detail", handleNacosConfigDetail)
	mux.HandleFunc("/api/nacos/config/publish", handleNacosPublish)
	mux.HandleFunc("/api/nacos/config/delete", handleNacosDelete)

	mux.HandleFunc("/api/backups", handleListBackups)           // GET
	mux.HandleFunc("/api/backups/create", handleCreateBackup)   // POST
	mux.HandleFunc("/api/backups/delete", handleDeleteBackup)   // POST
	mux.HandleFunc("/api/backups/restore", handleRestoreBackup) // POST

	mux.HandleFunc("/api/monitor/query_range", handleQueryRange)
	// --- WebSocket ---
	mux.HandleFunc("/api/ws", ws.HandleWebsocket)

	// --- 静态资源 ---
	fsUploads := http.FileServer(http.Dir(uploadPath))
	mux.Handle("/download/", http.StripPrefix("/download/", fsUploads))
	mux.Handle("/", http.FileServer(http.FS(assets)))
}

// 辅助函数：广播最新系统视图
func broadcastUpdate() {
	// 需要传入 instManager 来组装完整视图
	data := sysManager.GetFullView(instManager)
	ws.BroadcastSystems(data)
}
