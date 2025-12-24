package code

// ====================================================
// 错误码定义
// ====================================================

const (
	// 0: 成功
	Success = 0

	// 10xxx: 通用错误
	ServerError      = 10001
	ParamError       = 10002
	DatabaseError    = 10003
	NetworkError     = 10004
	MethodNotAllowed = 10005
	InvalidJSON      = 10006
	Unauthorized     = 10007 // 未登录
	Forbidden        = 10008 // 无权限

	// 20xxx: 节点管理
	NodeOffline        = 20001
	NodeNotFound       = 20002
	NodeRegisterFailed = 20003
	NodeExecFailed     = 20004

	// 30xxx: 业务系统 & 实例
	SystemNotFound   = 30001
	InstanceNotFound = 30002
	DeployFailed     = 30003
	ActionFailed     = 30004
	ModuleNotFound   = 30005

	// 40xxx: 服务包管理
	PackageUploadFailed = 40001
	PackageNotFound     = 40002
	PackageExist        = 40003
	PackageInvalid      = 40004 // 格式错误或缺少 service.json
	PackageDeleteFailed = 40005

	// 50xxx: 监控 & 告警 & 配置
	NacosError      = 50001
	AlertRuleError  = 50002
	LogFileNotFound = 50003
)

// ====================================================
// 错误信息映射
// ====================================================

var Msg = map[int]string{
	Success:          "操作成功",
	ServerError:      "服务器内部错误",
	ParamError:       "参数错误",
	DatabaseError:    "数据库操作失败",
	NetworkError:     "网络连接失败",
	MethodNotAllowed: "不支持该请求方法",
	InvalidJSON:      "无效的 JSON 格式",
	Unauthorized:     "未授权，请登录",
	Forbidden:        "无权限执行此操作",

	NodeOffline:        "节点不在线",
	NodeNotFound:       "节点不存在",
	NodeRegisterFailed: "节点注册失败",
	NodeExecFailed:     "远程指令执行失败",

	SystemNotFound:   "业务系统不存在",
	InstanceNotFound: "实例不存在",
	DeployFailed:     "服务部署失败",
	ActionFailed:     "实例操作失败",
	ModuleNotFound:   "服务组件定义不存在",

	PackageUploadFailed: "服务包上传失败",
	PackageNotFound:     "服务包不存在",
	PackageExist:        "服务包版本已存在",
	PackageInvalid:      "服务包格式无效(缺少service.json?)",
	PackageDeleteFailed: "服务包删除失败",

	NacosError:      "Nacos 交互失败",
	AlertRuleError:  "告警规则操作失败",
	LogFileNotFound: "日志文件不存在",
}

// GetMsg 获取错误码对应的默认信息
func GetMsg(code int) string {
	msg, ok := Msg[code]
	if ok {
		return msg
	}
	return Msg[ServerError]
}
