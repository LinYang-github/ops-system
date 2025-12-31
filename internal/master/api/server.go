package api

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"time"

	"ops-system/internal/master/db"
	"ops-system/internal/master/manager"
	"ops-system/internal/master/middleware"
	"ops-system/internal/master/monitor"
	"ops-system/internal/master/scheduler"
	"ops-system/internal/master/ws"

	"ops-system/pkg/config"
	"ops-system/pkg/storage"
	"ops-system/pkg/utils"
)

// 全局 Manager 实例变量
// 注意：虽然定义在这里，但在 Handler 中我们是通过 ServerHandler 结构体注入使用的，
// 这里保留变量主要是为了方便某些非 Handler 的后台任务（如 server.go 里的匿名 func）引用。
var (
	sysManager    *manager.SystemManager
	instManager   *manager.InstanceManager
	logManager    *manager.LogManager
	pkgManager    *manager.PackageManager
	nodeManager   *manager.NodeManager
	configManager *manager.ConfigManager
	backupManager *manager.BackupManager
	exportManager *manager.ExportManager
	monitorStore  *monitor.MemoryTSDB
	alertManager  *manager.AlertManager
)

// 全局上传路径，供静态资源服务使用
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
func StartMasterServer(cfg *config.MasterConfig, assets fs.FS) error {
	uploadPath = cfg.Storage.UploadDir

	// 1. 初始化数据库
	database := db.InitDB(cfg.Server.DBPath)

	// 2. 初始化监控存储
	monitorStore = monitor.NewMemoryTSDB()

	// 3. 初始化文件存储 Provider
	var storeProvider storage.Provider
	var err error

	if cfg.Storage.Type == "minio" {
		log.Printf("Using MinIO Storage: %s/%s", cfg.Storage.Minio.Endpoint, cfg.Storage.Minio.Bucket)
		storeProvider, err = storage.NewMinioProvider(
			cfg.Storage.Minio.Endpoint,
			cfg.Storage.Minio.AK,
			cfg.Storage.Minio.SK,
			cfg.Storage.Minio.Bucket,
			false, // useSSL
		)
	} else {
		log.Printf("Using Local Storage: %s", cfg.Storage.UploadDir)
		storeProvider = storage.NewLocalProvider(cfg.Storage.UploadDir)
	}

	if err != nil {
		return fmt.Errorf("init storage failed: %v", err)
	}

	// 4. 初始化基础环境 (使用启动参数作为初始值)
	utils.InitHTTPClient(cfg.Logic.HTTPClientTimeout, cfg.Auth.SecretKey)

	// 5. 初始化所有 Manager (单次实例化，依赖注入)
	logManager = manager.NewLogManager(database)
	sysManager = manager.NewSystemManager(database)
	instManager = manager.NewInstanceManager(database)
	pkgManager = manager.NewPackageManager(database, storeProvider) // 传入 DB 和 Storage
	configManager = manager.NewConfigManager(database)
	backupManager = manager.NewBackupManager(database, cfg.Storage.UploadDir)
	exportManager = manager.NewExportManager(sysManager, pkgManager, instManager, cfg.Storage.UploadDir)

	// 初始 NodeManager (使用 CLI/Config 中的默认阈值)
	nodeManager = manager.NewNodeManager(database, monitorStore, cfg.Logic.NodeOfflineThreshold)

	// AlertManager 依赖上述已创建的实例
	alertManager = manager.NewAlertManager(database, nodeManager, instManager)

	// 6. 尝试加载动态配置 (热更新覆盖)
	if globalCfg, err := configManager.GetGlobalConfig(); err == nil {
		log.Printf("Loaded dynamic config from DB, applying updates...")

		// 更新 HTTP Client 超时
		if globalCfg.Logic.HTTPClientTimeout > 0 {
			utils.SetTimeout(time.Duration(globalCfg.Logic.HTTPClientTimeout) * time.Second)
		}

		// 更新 NodeManager 阈值 (仅修改内部状态，不重新创建实例)
		if globalCfg.Logic.NodeOfflineThreshold > 0 {
			nodeManager.SetOfflineThreshold(time.Duration(globalCfg.Logic.NodeOfflineThreshold) * time.Second)
		}
	} else {
		// 如果 DB 没配置，就保持步骤 4 和 5 中的默认状态，不需要做任何事
		log.Printf("No dynamic config in DB, using startup flags/defaults.")
	}

	// 7. 启动后台任务：日志自动清理
	go func() {
		// 启动时先执行一次
		currentCfg, _ := configManager.GetGlobalConfig()
		if currentCfg != nil {
			logManager.CleanupOldLogs(currentCfg.Log.RetentionDays)
		}

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			cfg, err := configManager.GetGlobalConfig()
			if err == nil {
				logManager.CleanupOldLogs(cfg.Log.RetentionDays)
			}
		}
	}()

	// 8. 初始化调度器
	sched := scheduler.NewScheduler()

	// 9. 初始化全局 Handler 容器
	serverHandler := NewServerHandler(
		sysManager,
		instManager,
		nodeManager,
		logManager,
		pkgManager,
		configManager,
		alertManager,
		backupManager,
		exportManager,
		monitorStore,
		sched,
		cfg.Auth.SecretKey, // 传入 Secret 用于登录接口
	)

	// 10. 启动 WebSocket Hub
	go ws.GlobalHub.Run()

	// 11. 创建路由器并注册路由
	mux := http.NewServeMux()
	registerRoutes(mux, serverHandler, cfg.Storage.UploadDir, assets)

	log.Printf("Master UI & API running on %s", cfg.Server.Port)

	// =========================================================
	// 中间件链式组装
	// =========================================================

	// 1. 起点是路由器 (Mux)
	var handler http.Handler = mux

	// 2. 包裹鉴权中间件 (Auth)
	// AuthMiddleware 接收 handler，返回包裹后的 handler
	handler = middleware.AuthMiddleware(cfg.Auth.SecretKey)(handler)

	// 3. 包裹超时中间件 (Timeout)
	// 只有当配置了超时时间才包裹
	// 注意：这里一定要确保 handler 不为 nil (上面已经赋值了，所以是安全的)
	if cfg.Server.APITimeout > 0 { // 假设你在 config 中定义了 APITimeout
		timeoutDuration := time.Duration(cfg.Server.APITimeout) * time.Second
		handler = middleware.TimeoutMiddleware(timeoutDuration)(handler)
	} else if cfg.Logic.HTTPClientTimeout > 0 {
		// 兼容逻辑：如果 Server.APITimeout 没配，暂用 HTTPClientTimeout 或默认值
		// 建议在 config/loader.go 里给 Server.APITimeout 一个默认值 10
		timeoutDuration := time.Duration(cfg.Logic.HTTPClientTimeout) * time.Second
		handler = middleware.TimeoutMiddleware(timeoutDuration)(handler)
	}

	server := &http.Server{
		Addr:         cfg.Server.Port,
		Handler:      handler, // 使用最终组装好的 Handler
		ReadTimeout:  0,       // 必须为 0，否则大文件/WS 会断
		WriteTimeout: 0,
	}

	return server.ListenAndServe()
}

// registerRoutes 注册所有路由
// h: 包含所有业务逻辑的 Handler 实例
func registerRoutes(mux *http.ServeMux, h *ServerHandler, uploadPath string, assets fs.FS) {
	// --- Node 相关 ---
	mux.HandleFunc("/api/worker/heartbeat", h.HandleHeartbeat)
	mux.HandleFunc("/api/nodes", h.ListNodes)
	mux.HandleFunc("/api/nodes/add", h.AddNode)
	mux.HandleFunc("/api/nodes/delete", h.DeleteNode)
	mux.HandleFunc("/api/nodes/rename", h.RenameNode)
	mux.HandleFunc("/api/nodes/reset_name", h.ResetNodeName)
	mux.HandleFunc("/api/ctrl/cmd", h.TriggerCmd)
	mux.HandleFunc("/api/node/terminal", h.HandleNodeTerminal) // Web终端
	mux.HandleFunc("/api/nodes/clean_cache", h.CleanNodeCache)

	// --- System 配置相关 ---
	mux.HandleFunc("/api/systems", h.GetSystems)
	mux.HandleFunc("/api/systems/create", h.CreateSystem)
	mux.HandleFunc("/api/systems/delete", h.DeleteSystem)
	mux.HandleFunc("/api/systems/module/add", h.CreateSystemModule)
	mux.HandleFunc("/api/systems/module/delete", h.DeleteSystemModule)
	mux.HandleFunc("/api/systems/export", h.ExportSystem)

	// --- Instance 运行相关 ---
	mux.HandleFunc("/api/deploy", h.DeployInstance)
	mux.HandleFunc("/api/deploy/external", h.RegisterExternal) // 纳管
	mux.HandleFunc("/api/instance/action", h.InstanceAction)
	mux.HandleFunc("/api/instance/status_report", h.WorkerStatusReport)
	mux.HandleFunc("/api/systems/action", h.SystemAction) // 批量操作

	// --- Package 相关 ---
	mux.HandleFunc("/api/upload", h.UploadPackage)
	mux.HandleFunc("/api/packages", h.ListPackages)
	mux.HandleFunc("/api/packages/delete", h.DeletePackage)
	mux.HandleFunc("/api/packages/manifest", h.GetPackageManifest)
	// 上传预签名与回调
	mux.HandleFunc("/api/package/presign", h.PresignUpload)
	mux.HandleFunc("/api/package/callback", h.UploadCallback)
	mux.HandleFunc("/api/upload/direct", h.HandleDirectUpload)

	// --- Log 相关 ---
	mux.HandleFunc("/api/logs", h.GetOpLogs)
	mux.HandleFunc("/api/instance/logs/files", h.GetInstanceLogFiles)
	mux.HandleFunc("/api/instance/logs/stream", h.InstanceLogStream)

	// --- Config Center (Nacos) 相关 ---
	mux.HandleFunc("/api/nacos/settings", h.NacosSettings)
	mux.HandleFunc("/api/nacos/namespaces", h.NacosNamespaces)
	mux.HandleFunc("/api/nacos/configs", h.NacosConfigs)
	mux.HandleFunc("/api/nacos/config/detail", h.NacosConfigDetail)
	mux.HandleFunc("/api/nacos/config/publish", h.NacosPublish)
	mux.HandleFunc("/api/nacos/config/delete", h.NacosDelete)

	// --- Global Settings ---
	mux.HandleFunc("/api/settings/global", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.GetGlobalConfig(w, r)
		} else if r.Method == http.MethodPost {
			h.UpdateGlobalConfig(w, r)
		}
	})

	// --- Backup 相关 ---
	mux.HandleFunc("/api/backups", h.ListBackups)
	mux.HandleFunc("/api/backups/create", h.CreateBackup)
	mux.HandleFunc("/api/backups/delete", h.DeleteBackup)
	mux.HandleFunc("/api/backups/restore", h.RestoreBackup)

	// --- Monitor 相关 ---
	mux.HandleFunc("/api/monitor/query_range", h.QueryRange)

	// --- Alert 相关 ---
	mux.HandleFunc("/api/alerts/rules", h.ListRules)
	mux.HandleFunc("/api/alerts/rules/add", h.AddRule)
	mux.HandleFunc("/api/alerts/rules/delete", h.DeleteRule)
	mux.HandleFunc("/api/alerts/events", h.GetAlerts)
	mux.HandleFunc("/api/alerts/events/delete", h.DeleteEvent)
	mux.HandleFunc("/api/alerts/events/clear", h.ClearEvents)

	mux.HandleFunc("/api/login", h.HandleLogin)
	mux.HandleFunc("/api/maintenance/cleanup_all_cache", h.CleanAllNodesCache)
	mux.HandleFunc("/api/maintenance/scan_orphans", h.ScanAllOrphans)
	mux.HandleFunc("/api/maintenance/delete_orphans", h.DeleteOrphans)
	// --- WebSocket ---
	mux.HandleFunc("/api/ws", ws.HandleWebsocket)

	// --- 静态资源 ---
	// 文件下载 (uploadPath 来自参数，不再依赖全局变量)
	fsUploads := http.FileServer(http.Dir(uploadPath))
	mux.Handle("/download/", http.StripPrefix("/download/", fsUploads))

	// 前端页面
	mux.Handle("/", http.FileServer(http.FS(assets)))
}
