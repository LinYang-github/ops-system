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
	"ops-system/internal/master/ws"

	"ops-system/pkg/config"
	"ops-system/pkg/storage"
	"ops-system/pkg/utils"
)

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
	// 1. 初始化数据库
	database := db.InitDB(cfg.Server.DBPath)

	// 2. 初始化监控存储 (内存 TSDB)
	monitorStore := monitor.NewMemoryTSDB()

	// 3. 初始化文件存储 Provider (Local 或 MinIO)
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

	utils.InitHTTPClient(cfg.Logic.HTTPClientTimeout, cfg.Auth.SecretKey)

	// 4. 初始化所有 Manager (依赖注入)
	// 注意顺序：底层依赖先初始化
	logMgr := manager.NewLogManager(database)
	sysMgr := manager.NewSystemManager(database)
	instMgr := manager.NewInstanceManager(database)
	nodeMgr := manager.NewNodeManager(database, monitorStore, cfg.Logic.NodeOfflineThreshold)
	pkgMgr := manager.NewPackageManager(storeProvider)
	configMgr := manager.NewConfigManager(database)
	backupMgr := manager.NewBackupManager(database, cfg.Storage.UploadDir)
	alertMgr := manager.NewAlertManager(database, nodeMgr, instMgr)

	globalCfg, err := configMgr.GetGlobalConfig()
	if err == nil {
		log.Printf("Loaded global config from DB: %+v", globalCfg)
		// 应用配置
		utils.InitHTTPClient(time.Duration(globalCfg.Logic.HTTPClientTimeout)*time.Second, cfg.Auth.SecretKey)
		// 注意：NodeManager 初始化时传入动态值
		nodeMgr = manager.NewNodeManager(database, monitorStore, time.Duration(globalCfg.Logic.NodeOfflineThreshold)*time.Second)
	} else {
		log.Printf("Using default/flag config")
		utils.InitHTTPClient(cfg.Logic.HTTPClientTimeout, cfg.Auth.SecretKey)
		nodeMgr = manager.NewNodeManager(database, monitorStore, cfg.Logic.NodeOfflineThreshold)
	}

	// 启动日志自动清理任务 (每天执行一次)
	go func() {
		// 启动时先执行一次
		currentCfg, _ := configMgr.GetGlobalConfig()
		if currentCfg != nil {
			logMgr.CleanupOldLogs(currentCfg.Log.RetentionDays)
		}

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			// 每次执行时重新读取最新配置
			cfg, err := configMgr.GetGlobalConfig()
			if err == nil {
				logMgr.CleanupOldLogs(cfg.Log.RetentionDays)
			}
		}
	}()

	// 5. 初始化全局 Handler 容器
	// 将所有 Manager 注入到 Handler 中，彻底消除全局变量
	serverHandler := NewServerHandler(
		sysMgr,
		instMgr,
		nodeMgr,
		logMgr,
		pkgMgr,
		configMgr,
		alertMgr,
		backupMgr,
		monitorStore,
	)

	// 6. 启动 WebSocket Hub
	go ws.GlobalHub.Run()

	// 7. 创建路由器并注册路由
	mux := http.NewServeMux()
	registerRoutes(mux, serverHandler, cfg.Storage.UploadDir, assets)

	log.Printf("Master UI & API running on %s", cfg.Server.Port)

	authHandler := middleware.AuthMiddleware(cfg.Auth.SecretKey)(mux)

	server := &http.Server{
		Addr:         cfg.Server.Port,
		Handler:      authHandler, // 使用包装后的 Handler
		ReadTimeout:  0,
		WriteTimeout: 0,
	}

	return server.ListenAndServe()
}

// registerRoutes 注册所有路由
// h: 包含所有业务逻辑的 Handler 实例
func registerRoutes(mux *http.ServeMux, h *ServerHandler, uploadPath string, assets fs.FS) {
	// --- Node 相关 (node_handler.go) ---
	mux.HandleFunc("/api/worker/heartbeat", h.HandleHeartbeat)
	mux.HandleFunc("/api/nodes", h.ListNodes)
	mux.HandleFunc("/api/nodes/add", h.AddNode)
	mux.HandleFunc("/api/nodes/delete", h.DeleteNode)
	mux.HandleFunc("/api/nodes/rename", h.RenameNode)
	mux.HandleFunc("/api/nodes/reset_name", h.ResetNodeName)
	mux.HandleFunc("/api/ctrl/cmd", h.TriggerCmd)

	// --- System 配置相关 (system_handler.go) ---
	mux.HandleFunc("/api/systems", h.GetSystems)
	mux.HandleFunc("/api/systems/create", h.CreateSystem)
	mux.HandleFunc("/api/systems/delete", h.DeleteSystem)
	mux.HandleFunc("/api/systems/module/add", h.CreateSystemModule)
	mux.HandleFunc("/api/systems/module/delete", h.DeleteSystemModule)

	// --- Instance 运行相关 (instance_handler.go) ---
	mux.HandleFunc("/api/deploy", h.DeployInstance)
	mux.HandleFunc("/api/deploy/external", h.RegisterExternal) // 纳管
	mux.HandleFunc("/api/instance/action", h.InstanceAction)
	mux.HandleFunc("/api/instance/status_report", h.WorkerStatusReport)
	mux.HandleFunc("/api/systems/action", h.SystemAction) // 批量操作

	// --- Package 相关 (package_handler.go) ---
	mux.HandleFunc("/api/upload", h.UploadPackage)
	mux.HandleFunc("/api/packages", h.ListPackages)
	mux.HandleFunc("/api/packages/delete", h.DeletePackage)
	mux.HandleFunc("/api/packages/manifest", h.GetPackageManifest)

	// --- Log 相关 (log_handler.go) ---
	mux.HandleFunc("/api/logs", h.GetOpLogs)
	mux.HandleFunc("/api/instance/logs/files", h.GetInstanceLogFiles)
	mux.HandleFunc("/api/instance/logs/stream", h.InstanceLogStream)

	// --- Config Center (Nacos) 相关 (config_handler.go) ---
	mux.HandleFunc("/api/nacos/settings", h.NacosSettings)
	mux.HandleFunc("/api/nacos/namespaces", h.NacosNamespaces)
	mux.HandleFunc("/api/nacos/configs", h.NacosConfigs)
	mux.HandleFunc("/api/nacos/config/detail", h.NacosConfigDetail)
	mux.HandleFunc("/api/nacos/config/publish", h.NacosPublish)
	mux.HandleFunc("/api/nacos/config/delete", h.NacosDelete)

	mux.HandleFunc("/api/settings/global", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			h.GetGlobalConfig(w, r)
		} else if r.Method == http.MethodPost {
			h.UpdateGlobalConfig(w, r)
		}
	})

	// --- Backup 相关 (backup_handler.go) ---
	mux.HandleFunc("/api/backups", h.ListBackups)
	mux.HandleFunc("/api/backups/create", h.CreateBackup)
	mux.HandleFunc("/api/backups/delete", h.DeleteBackup)
	mux.HandleFunc("/api/backups/restore", h.RestoreBackup)

	// --- Monitor 相关 (monitor_handler.go) ---
	mux.HandleFunc("/api/monitor/query_range", h.QueryRange)

	// --- Alert 相关 (alert_handler.go) ---
	mux.HandleFunc("/api/alerts/rules", h.ListRules)
	mux.HandleFunc("/api/alerts/rules/add", h.AddRule)
	mux.HandleFunc("/api/alerts/rules/delete", h.DeleteRule)
	mux.HandleFunc("/api/alerts/events", h.GetAlerts)
	mux.HandleFunc("/api/alerts/events/delete", h.DeleteEvent)
	mux.HandleFunc("/api/alerts/events/clear", h.ClearEvents)

	// --- WebSocket ---
	mux.HandleFunc("/api/ws", ws.HandleWebsocket)
	mux.HandleFunc("/api/node/terminal", h.HandleNodeTerminal)

	// --- 静态资源 ---
	// 文件下载 (uploadPath 来自参数，不再依赖全局变量)
	fsUploads := http.FileServer(http.Dir(uploadPath))
	mux.Handle("/download/", http.StripPrefix("/download/", fsUploads))

	mux.HandleFunc("/api/package/presign", h.PresignUpload)
	mux.HandleFunc("/api/package/callback", h.UploadCallback)
	mux.HandleFunc("/api/upload/direct", h.HandleDirectUpload)
	// 前端页面
	mux.Handle("/", http.FileServer(http.FS(assets)))
}
