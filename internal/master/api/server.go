package api

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"

	"ops-system/internal/master/db"
	"ops-system/internal/master/manager"
	"ops-system/internal/master/monitor"
	"ops-system/internal/master/ws"
	"ops-system/pkg/storage"
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
	alertManager  *manager.AlertManager
)

var uploadPath string

// 定义配置结构体
type MinioConfig struct {
	Endpoint string
	AK, SK   string
	Bucket   string
}

type ServerConfig struct {
	Port      string
	UploadDir string
	DBPath    string
	StoreType string // "local" | "minio"
	MinioConfig
}

// StartMasterServer 启动 Master HTTP 服务
func StartMasterServer(cfg ServerConfig, assets fs.FS) error {
	uploadPath = cfg.UploadDir // 依然保留，用于 Local 模式或临时文件

	// 1. 初始化数据库 (传入路径)
	database := db.InitDB(cfg.DBPath)

	// 1. 初始化 Monitor Store
	monitorStore = monitor.NewMemoryTSDB()

	// 2. 初始化 Storage Provider
	var storeProvider storage.Provider
	var err error

	if cfg.StoreType == "minio" {
		log.Printf("Using MinIO Storage: %s/%s", cfg.MinioConfig.Endpoint, cfg.MinioConfig.Bucket)
		storeProvider, err = storage.NewMinioProvider(
			cfg.MinioConfig.Endpoint,
			cfg.MinioConfig.AK,
			cfg.MinioConfig.SK,
			cfg.MinioConfig.Bucket,
			false, // useSSL (可加参数控制)
		)
	} else {
		log.Printf("Using Local Storage: %s", cfg.UploadDir)
		storeProvider = storage.NewLocalProvider(cfg.UploadDir)
	}

	if err != nil {
		return fmt.Errorf("init storage failed: %v", err)
	}

	// 2. 初始化 Managers (依赖注入)
	logManager = manager.NewLogManager(database)
	sysManager = manager.NewSystemManager(database)
	instManager = manager.NewInstanceManager(database)
	nodeManager = manager.NewNodeManager(database, monitorStore)
	pkgManager = manager.NewPackageManager(storeProvider)
	configManager = manager.NewConfigManager(database)
	backupManager = manager.NewBackupManager(database, uploadPath)
	nodeManager = manager.NewNodeManager(database, monitorStore)
	alertManager = manager.NewAlertManager(database, nodeManager, instManager)

	// 3. 启动 WebSocket Hub
	go ws.GlobalHub.Run()

	// 4. 创建路由器
	mux := http.NewServeMux()
	registerRoutes(mux, assets)

	log.Printf("Master UI & API running on %s", cfg.Port)

	server := &http.Server{
		Addr:         cfg.Port,
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
	mux.HandleFunc("/api/instance/logs/files", handleGetInstanceLogFiles)
	mux.HandleFunc("/api/instance/logs/stream", handleInstanceLogStream)

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

	mux.HandleFunc("/api/alerts/rules", handleListRules)
	mux.HandleFunc("/api/alerts/rules/add", handleAddRule)
	mux.HandleFunc("/api/alerts/rules/delete", handleDeleteRule)
	mux.HandleFunc("/api/alerts/events", handleGetAlerts)
	mux.HandleFunc("/api/alerts/events/delete", handleDeleteEvent)
	mux.HandleFunc("/api/alerts/events/clear", handleClearEvents)
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
