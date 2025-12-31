package api

import (
	"ops-system/internal/master/manager"
	"ops-system/internal/master/monitor"
	"ops-system/internal/master/scheduler"
	"ops-system/internal/master/transport" // [新增] 引入 transport 包以使用 WorkerGateway
)

// ServerHandler 持有所有业务逻辑依赖
// 这种结构允许我们在测试时注入 Mock 的 Manager
type ServerHandler struct {
	sysMgr       *manager.SystemManager
	instMgr      *manager.InstanceManager
	nodeMgr      *manager.NodeManager
	logMgr       *manager.LogManager
	pkgMgr       *manager.PackageManager
	configMgr    *manager.ConfigManager
	alertMgr     *manager.AlertManager
	backupMgr    *manager.BackupManager
	monitorStore *monitor.MemoryTSDB
	exportMgr    *manager.ExportManager
	scheduler    *scheduler.Scheduler
	gateway      *transport.WorkerGateway
	secretKey    string
}

// NewServerHandler 构造函数
func NewServerHandler(
	sys *manager.SystemManager,
	inst *manager.InstanceManager,
	node *manager.NodeManager,
	log *manager.LogManager,
	pkg *manager.PackageManager,
	cfg *manager.ConfigManager,
	alert *manager.AlertManager,
	backup *manager.BackupManager,
	export *manager.ExportManager,
	monitor *monitor.MemoryTSDB,
	scheduler *scheduler.Scheduler,
	gateway *transport.WorkerGateway,
	secretKey string,
) *ServerHandler {
	return &ServerHandler{
		sysMgr:       sys,
		instMgr:      inst,
		nodeMgr:      node,
		logMgr:       log,
		pkgMgr:       pkg,
		configMgr:    cfg,
		alertMgr:     alert,
		backupMgr:    backup,
		exportMgr:    export,
		monitorStore: monitor,
		scheduler:    scheduler,
		gateway:      gateway,
		secretKey:    secretKey,
	}
}
