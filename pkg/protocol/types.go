package protocol

// ==========================================
// 1. 节点基础信息 (Node)
// ==========================================

// NodeInfo 静态信息
// NodeInfo 节点信息 (持久化存储)
type NodeInfo struct {
	ID        string `json:"id"` // [新增] 唯一标识 UUID
	IP        string `json:"ip"` // 仅用于通信地址，不再是主键
	Port      int    `json:"port"`
	Hostname  string `json:"hostname"` // 机器原本的主机名
	Name      string `json:"name"`     // 用户自定义的节点名称 (别名)
	MacAddr   string `json:"mac_addr"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	CPUCores  int    `json:"cpu_cores"`
	MemTotal  uint64 `json:"mem_total"`
	DiskTotal uint64 `json:"disk_total"`

	// 状态字段
	Status        string `json:"status"`         // "online", "offline", "planned"
	LastHeartbeat int64  `json:"last_heartbeat"` // 上次心跳时间

	// 实时监控 (存内存，不存DB，或者存DB为了简单)
	// 为了统一架构，建议基础信息存DB，高频监控数据存内存(同Instance)
	// 这里简化处理：UpdateHeartbeat 时顺便更新到 DB，因为节点只有几百个，频率不高
	CPUUsage    float64 `json:"cpu_usage"`
	MemUsage    float64 `json:"mem_usage"`
	NetInSpeed  float64 `json:"net_in_speed"`
	NetOutSpeed float64 `json:"net_out_speed"`
}

// NodeStatus 动态监控信息
type NodeStatus struct {
	CPUUsage    float64 `json:"cpu_usage"`     // 百分比
	MemUsage    float64 `json:"mem_usage"`     // 百分比
	DiskUsage   float64 `json:"disk_usage"`    // 百分比
	NetInSpeed  float64 `json:"net_in_speed"`  // KB/s
	NetOutSpeed float64 `json:"net_out_speed"` // KB/s
	Uptime      uint64  `json:"uptime"`        // 秒
	Time        int64   `json:"time"`
}

// RegisterRequest 注册/心跳请求
type RegisterRequest struct {
	Port   int        `json:"port"` // Worker 监听的端口
	Info   NodeInfo   `json:"info"`
	Status NodeStatus `json:"status"`
}

// ==========================================
// 2. 指令与控制 (Command)
// ==========================================

// CommandRequest 执行 CMD 指令
type CommandRequest struct {
	Command string `json:"command"`
}

// ==========================================
// 3. 服务包管理 (Package)
// ==========================================

// ServiceManifest 对应 zip 包内的 service.json 文件
type ServiceManifest struct {
	Name           string            `json:"name"`            // 服务名称
	Version        string            `json:"version"`         // 版本号
	Entrypoint     string            `json:"entrypoint"`      // 启动入口
	Args           []string          `json:"args"`            // 启动参数
	StopEntrypoint string            `json:"stop_entrypoint"` // 停止脚本入口 (可选, 如 bin/stop.sh)
	StopArgs       []string          `json:"stop_args"`       // 停止参数 (可选)
	Env            map[string]string `json:"env"`             // 环境变量

	// 日志文件映射
	// Key: 日志显示名称 (如 "Access Log", "Error Log")
	// Value: 日志绝对路径 或 相对工作目录的路径 (如 "/var/log/nginx/access.log", "logs/gc.log")
	LogPaths map[string]string `json:"log_paths"`

	// --- 新增：纳管专用字段 ---
	IsExternal      bool   `json:"is_external"`       // 是否为纳管服务
	ExternalWorkDir string `json:"external_work_dir"` // 外部服务的真实工作目录
	PidStrategy     string `json:"pid_strategy"`      // spawn / match
	ProcessName     string `json:"process_name"`      // match 模式下的进程名
	// -----------------------

	Description string `json:"description"` // 描述
	OS          string `json:"os"`          // 适用系统

	// 就绪检测类型: "tcp", "http", "time", "none"
	ReadinessType string `json:"readiness_type"`

	// 检测目标:
	// type="tcp" -> ":8848"
	// type="http" -> "http://localhost:8080/health"
	// type="time" -> "30" (秒)
	ReadinessTarget string `json:"readiness_target"`

	// 检测超时时间 (秒)，超过这个时间还没就绪则视为启动失败
	ReadinessTimeout int `json:"readiness_timeout"`
}

// PackageInfo 用于前端展示的服务包列表信息
type PackageInfo struct {
	Name       string   `json:"name"`
	Versions   []string `json:"versions"`
	LastUpload int64    `json:"last_upload"`
}

// ==========================================
// 4. 业务系统与实例 (System & Instance)
// ==========================================

// SystemInfo 系统逻辑分组
type SystemInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreateTime  int64  `json:"create_time"`
}

// InstanceInfo 运行实例信息 (用于 API 返回给前端)
type InstanceInfo struct {
	ID             string `json:"id"`
	SystemID       string `json:"system_id"`
	NodeID         string `json:"node_id"`
	ServiceName    string `json:"service_name"`
	ServiceVersion string `json:"service_version"`

	// --- 持久化字段 (存 DB) ---
	Status string `json:"status"` // running, stopped
	PID    int    `json:"pid"`
	Uptime int64  `json:"uptime"`

	// --- 实时监控字段 (存 内存) ---
	CpuUsage float64 `json:"cpu_usage"`
	MemUsage uint64  `json:"mem_usage"`
	IoRead   uint64  `json:"io_read"`
	IoWrite  uint64  `json:"io_write"`
}

// InstanceStatusReport Worker 上报的状态 (增加监控字段)
type InstanceStatusReport struct {
	InstanceID string `json:"instance_id"`
	Status     string `json:"status"`
	PID        int    `json:"pid"`
	Uptime     int64  `json:"uptime"`

	// 新增监控数据
	CpuUsage float64 `json:"cpu_usage"`
	MemUsage uint64  `json:"mem_usage"`
	IoRead   uint64  `json:"io_read"`
	IoWrite  uint64  `json:"io_write"`
}

// SystemModule 系统服务定义 (规划阶段)
// 表示：某个系统 "包含" 某个服务包的特定版本
type SystemModule struct {
	ID             string `json:"id"`
	SystemID       string `json:"system_id"`
	ModuleName     string `json:"module_name"`
	PackageName    string `json:"package_name"`
	PackageVersion string `json:"package_version"`
	Description    string `json:"description"`

	// 【新增】编排与覆盖配置
	StartOrder       int    `json:"start_order"`    // 启动顺序 (1, 2, 3...)
	ReadinessType    string `json:"readiness_type"` // 覆盖默认值
	ReadinessTarget  string `json:"readiness_target"`
	ReadinessTimeout int    `json:"readiness_timeout"`
}

// SystemView 聚合视图 (用于前端展示)
type SystemView struct {
	*SystemInfo
	Modules   []*SystemModule `json:"modules"`   // 已定义的服务
	Instances []*InstanceInfo `json:"instances"` // 已运行的实例
}

// DeployRequest 部署请求 (Master -> Worker)
type DeployRequest struct {
	InstanceID  string            `json:"instance_id"`
	SystemName  string            `json:"system_name"` // 用于创建目录区分
	ServiceName string            `json:"service_name"`
	Version     string            `json:"version"`
	DownloadURL string            `json:"download_url"` // 包下载地址
	Entrypoint  string            `json:"entrypoint"`
	Args        []string          `json:"args"`
	Env         map[string]string `json:"env"`

	// 【新增】运行时配置 (Worker 需要把这些存下来，供 StartProcess 使用)
	ReadinessType    string `json:"readiness_type"`
	ReadinessTarget  string `json:"readiness_target"`
	ReadinessTimeout int    `json:"readiness_timeout"`
}

// InstanceActionRequest 实例控制请求 (Master -> Worker)
type InstanceActionRequest struct {
	InstanceID string `json:"instance_id"`
	Action     string `json:"action"` // "start", "stop", "destroy"
}

// OpLog 操作日志
type OpLog struct {
	ID         int64  `json:"id"`
	Operator   string `json:"operator"`    // 操作者 (目前暂无登录系统，存 IP)
	Action     string `json:"action"`      // 动作类型 (如: create_system, start_instance)
	TargetType string `json:"target_type"` // 对象类型 (system, instance, package)
	TargetName string `json:"target_name"` // 对象名称 (方便阅读)
	Detail     string `json:"detail"`      // 详情 JSON 或 文本
	Status     string `json:"status"`      // success, fail
	CreateTime int64  `json:"create_time"`
}

// LogQueryReq 查询请求
type LogQueryReq struct {
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
	Keyword  string `json:"keyword"` // 搜索 Action 或 TargetName
}

// LogQueryResp 查询响应
type LogQueryResp struct {
	Total int64    `json:"total"`
	List  []*OpLog `json:"list"`
}

// ExternalConfig 纳管服务的配置描述 (存为 config.json)
type ExternalConfig struct {
	Name     string `json:"name"`
	WorkDir  string `json:"work_dir"`  // 应用绝对路径
	StartCmd string `json:"start_cmd"` // 启动命令
	StopCmd  string `json:"stop_cmd"`  // 停止命令 (可选)

	// 进程识别策略
	PidStrategy string `json:"pid_strategy"` // "spawn" (直接启动) 或 "match" (启动后查找)
	ProcessName string `json:"process_name"` // 用于 match 策略的进程名关键字
}

// RegisterExternalRequest 注册纳管实例请求 (Master -> Worker)
type RegisterExternalRequest struct {
	InstanceID string         `json:"instance_id"`
	SystemName string         `json:"system_name"`
	Config     ExternalConfig `json:"config"`
}

// BackupFile 备份文件信息
type BackupFile struct {
	Name       string `json:"name"`        // 文件名 (e.g. backup_20231201.zip)
	Size       int64  `json:"size"`        // 大小 (Bytes)
	CreateTime int64  `json:"create_time"` // 创建时间
	WithFiles  bool   `json:"with_files"`  // 是否包含 uploads 文件
}

// 新增：日志文件列表响应
type LogFilesResp struct {
	InstanceID string   `json:"instance_id"`
	Files      []string `json:"files"` // e.g. ["Console Log", "Access Log"]
	Error      string   `json:"error,omitempty"`
}

// AlertRule 告警规则配置
type AlertRule struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`        // 规则名称
	TargetType string  `json:"target_type"` // "node", "instance"
	Metric     string  `json:"metric"`      // "cpu", "mem", "status"(offline/stopped)
	Condition  string  `json:"condition"`   // ">", "<", "="
	Threshold  float64 `json:"threshold"`   // 阈值
	Duration   int     `json:"duration"`    // 持续时间(秒)，防抖动
	Enabled    bool    `json:"enabled"`
}

// AlertEvent 告警历史/活跃事件
type AlertEvent struct {
	ID         int64   `json:"id"`
	RuleID     int64   `json:"rule_id"`
	RuleName   string  `json:"rule_name"`
	TargetType string  `json:"target_type"`
	TargetID   string  `json:"target_id"`   // NodeIP 或 InstanceID
	TargetName string  `json:"target_name"` //用于展示
	MetricVal  float64 `json:"metric_val"`  // 触发时的值
	Message    string  `json:"message"`
	Status     string  `json:"status"` // "firing", "resolved"
	StartTime  int64   `json:"start_time"`
	EndTime    int64   `json:"end_time"` // resolved 时更新
}

// HeartbeatResponse 心跳响应 (Master -> Worker)
type HeartbeatResponse struct {
	Code              int   `json:"code"`
	HeartbeatInterval int64 `json:"heartbeat_interval"` // 秒
	MonitorInterval   int64 `json:"monitor_interval"`   // 秒
}

// RunnerManifest 离线运行清单
type RunnerManifest struct {
	SystemName string         `json:"system_name"`
	ExportTime int64          `json:"export_time"`
	Modules    []RunnerModule `json:"modules"`
}

type RunnerModule struct {
	Name       string            `json:"name"`
	WorkDir    string            `json:"work_dir"` // 相对路径 e.g. "services/redis"
	Entrypoint string            `json:"entrypoint"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env"`
	StartOrder int               `json:"start_order"`

	// 停止配置
	StopEntrypoint string   `json:"stop_entrypoint"`
	StopArgs       []string `json:"stop_args"`
}

// OrphanScanRequest 孤儿扫描请求 (Master -> Worker)
type OrphanScanRequest struct {
	ValidSystems   []string `json:"valid_systems"`   // 合法的系统名称列表
	ValidInstances []string `json:"valid_instances"` // 合法的实例 ID 列表
}

// OrphanItem 扫描到的孤儿对象
type OrphanItem struct {
	Type      string `json:"type"`       // "system_dir" 或 "instance_dir"
	Path      string `json:"path"`       // 相对路径 e.g. "PaymentSys/gateway_inst-123"
	AbsPath   string `json:"abs_path"`   // 绝对路径
	Size      int64  `json:"size"`       // 占用空间
	IsRunning bool   `json:"is_running"` // 进程是否存活 (如果存活，建议不要删)
	Pid       int    `json:"pid"`        // 存活时的 PID
}

// OrphanScanResponse 扫描响应
type OrphanScanResponse struct {
	Items []OrphanItem `json:"items"`
}

// OrphanDeleteRequest 删除请求
type OrphanDeleteRequest struct {
	Items []string `json:"items"` // 要删除的相对路径列表
}

// [新增] 隧道启动请求 (Master -> Worker)
type TunnelStartRequest struct {
	SessionID  string `json:"session_id"`            // 会话ID
	Type       string `json:"type"`                  // "log" or "terminal"
	InstanceID string `json:"instance_id,omitempty"` // 用于日志
	LogKey     string `json:"log_key,omitempty"`     // 用于日志
	Rows       int    `json:"rows,omitempty"`        // 用于终端
	Cols       int    `json:"cols,omitempty"`        // 用于终端
}
