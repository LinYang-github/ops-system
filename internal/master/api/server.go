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
	"ops-system/internal/master/transport" // [新增] 引入 Transport 层
	"ops-system/internal/master/ws"

	"ops-system/pkg/config"
	"ops-system/pkg/storage"
	"ops-system/pkg/utils"
)

// 全局 Manager 实例变量
// 注意：虽然定义在这里，但在 Handler 中我们是通过 ServerHandler 结构体注入使用的
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
	// gateway       *transport.WorkerGateway // 可选：如果需要在 Server 级引用
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

	// 初始 NodeManager
	nodeManager = manager.NewNodeManager(database, monitorStore, cfg.Logic.NodeOfflineThreshold)

	// AlertManager 依赖上述已创建的实例
	alertManager = manager.NewAlertManager(database, nodeManager, instManager)

	// 6. [新增] 初始化 Worker Gateway (WebSocket 通信层)
	// Gateway 依赖 NodeManager 处理心跳
	workerGateway := transport.NewWorkerGateway(nodeManager, configManager, instManager, sysManager)

	// 7. 尝试加载动态配置 (热更新覆盖)
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
		log.Printf("No dynamic config in DB, using startup flags/defaults.")
	}

	// 8. 启动后台任务：日志自动清理
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

	// 9. 初始化调度器
	sched := scheduler.NewScheduler()

	// 10. 初始化全局 Handler 容器
	// 【关键修改】注入 workerGateway
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
		workerGateway,      // [新增] 注入 Gateway
		cfg.Auth.SecretKey, // 传入 Secret 用于登录接口
	)

	// 11. 启动 WebSocket Hub (用于前端推送)
	go ws.GlobalHub.Run()

	// 【修复点 1】确保 assets 不为 nil。如果 embed 失败，使用内存文件系统兜底
	var guiHandler http.Handler
	if assets == nil {
		log.Println("⚠️ [Warning] Assets is nil, UI will be disabled")
		guiHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "UI assets not found", http.StatusNotFound)
		})
	} else {
		guiHandler = http.FileServer(http.FS(assets))
	}

	// 12. 创建路由器并注册路由
	mux := http.NewServeMux()
	registerRoutes(mux, serverHandler, cfg.Storage.UploadDir, guiHandler)

	log.Printf("Master UI & API running on %s", cfg.Server.Port)

	// =========================================================
	// 中间件链式组装
	// =========================================================

	// 【修复点 2】正确处理超时时长转换
	var apiTimeout time.Duration
	if cfg.Server.APITimeout > 0 {
		apiTimeout = time.Duration(cfg.Server.APITimeout) * time.Second
	} else {
		// 注意：如果已经是 time.Duration 类型，不要再乘以 time.Second
		apiTimeout = cfg.Logic.HTTPClientTimeout
	}

	// 【修复点 3】仅在时长有效时包裹中间件
	var handler http.Handler = mux
	handler = middleware.AuthMiddleware(cfg.Auth.SecretKey)(handler)

	if apiTimeout > 0 {
		log.Printf("Final API Timeout set to: %v", apiTimeout)
		handler = middleware.TimeoutMiddleware(apiTimeout)(handler)
	}

	server := &http.Server{
		Addr:         cfg.Server.Port,
		Handler:      handler,
		ReadTimeout:  0, // 必须为 0，否则大文件/WS 会断
		WriteTimeout: 0,
	}

	return server.ListenAndServe()
}

// registerRoutes 注册所有路由
func registerRoutes(mux *http.ServeMux, h *ServerHandler, uploadPath string, guiHandler http.Handler) {

	// --- Node 相关 ---
	mux.HandleFunc("/api/nodes", h.ListNodes)
	mux.HandleFunc("/api/nodes/add", h.AddNode)
	mux.HandleFunc("/api/nodes/delete", h.DeleteNode)
	mux.HandleFunc("/api/nodes/rename", h.RenameNode)
	mux.HandleFunc("/api/nodes/reset_name", h.ResetNodeName)
	mux.HandleFunc("/api/ctrl/cmd", h.TriggerCmd)              // 已改为 WS 下发
	mux.HandleFunc("/api/node/terminal", h.HandleNodeTerminal) // 暂依赖直连 IP
	mux.HandleFunc("/api/nodes/wake", h.WakeNode)

	// --- System 配置相关 ---
	mux.HandleFunc("/api/systems", h.GetSystems)
	mux.HandleFunc("/api/systems/create", h.CreateSystem)
	mux.HandleFunc("/api/systems/delete", h.DeleteSystem)
	mux.HandleFunc("/api/systems/module/add", h.CreateSystemModule)
	mux.HandleFunc("/api/systems/module/delete", h.DeleteSystemModule)
	mux.HandleFunc("/api/systems/export", h.ExportSystem)

	// --- Instance 运行相关 ---
	mux.HandleFunc("/api/deploy", h.DeployInstance)            // 已改为 WS 下发
	mux.HandleFunc("/api/deploy/external", h.RegisterExternal) // 已改为 WS 下发
	mux.HandleFunc("/api/instance/action", h.InstanceAction)   // 已改为 WS 下发
	mux.HandleFunc("/api/systems/action", h.SystemAction)      // 批量操作

	// --- Package 相关 ---
	mux.HandleFunc("/api/upload", h.UploadPackage)
	mux.HandleFunc("/api/packages", h.ListPackages)
	mux.HandleFunc("/api/packages/delete", h.DeletePackage)
	mux.HandleFunc("/api/packages/manifest", h.GetPackageManifest)
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

	// --- Maintenance (维护) 相关---
	mux.HandleFunc("/api/maintenance/orphans", h.ScanOrphans)
	mux.HandleFunc("/api/maintenance/orphans/delete", h.DeleteOrphans)
	mux.HandleFunc("/api/maintenance/cache/clean", h.CleanCache)
	mux.HandleFunc("/api/maintenance/cleanup_all", h.CleanupAllCache)

	mux.HandleFunc("/api/maintenance/cleanup_all_cache", h.CleanupNodeCaches) // 对应 executeClean
	mux.HandleFunc("/api/maintenance/scan_orphans", h.ScanNodeOrphans)        // 对应 openGcDialog (POST)
	mux.HandleFunc("/api/maintenance/delete_orphans", h.DeleteNodeOrphans)    // 对应 executeDelete
	// --- WebSocket (Frontend) ---
	mux.HandleFunc("/api/ws", ws.HandleWebsocket)
	// --- 静态资源 ---
	// 文件下载
	fsUploads := http.FileServer(http.Dir(uploadPath))
	mux.Handle("/download/", http.StripPrefix("/download/", fsUploads))

	// 前端页面
	// 【修复点 4】确保路由注册时不出现 nil 指针调用
	if h.gateway != nil {
		mux.HandleFunc("/api/worker/ws", h.gateway.HandleConnection) // 现有的控制通道
		mux.HandleFunc("/api/worker/tunnel", h.gateway.HandleTunnel) // 数据隧道通道
	}

	// 静态资源使用传入的 handler
	mux.Handle("/", guiHandler)
}
