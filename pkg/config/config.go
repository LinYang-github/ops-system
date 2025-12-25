package config

import (
	"time"
)

// ================= Master Config =================

type MasterConfig struct {
	Server  ServerConfig  `mapstructure:"server"`
	Storage StorageConfig `mapstructure:"storage"`
	Logic   LogicConfig   `mapstructure:"logic"`
	Log     LogConfig     `mapstructure:"log"`
	Auth    AuthConfig    `mapstructure:"auth"`
}

type ServerConfig struct {
	Port         string        `mapstructure:"port"`
	DBPath       string        `mapstructure:"db_path"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type StorageConfig struct {
	Type      string      `mapstructure:"type"` // "local" or "minio"
	UploadDir string      `mapstructure:"upload_dir"`
	Minio     MinioConfig `mapstructure:"minio"`
}

type MinioConfig struct {
	Endpoint string `mapstructure:"endpoint"`
	AK       string `mapstructure:"ak"`
	SK       string `mapstructure:"sk"`
	Bucket   string `mapstructure:"bucket"`
	UseSSL   bool   `mapstructure:"use_ssl"`
}

type LogicConfig struct {
	NodeOfflineThreshold time.Duration `mapstructure:"node_offline_threshold"` // 节点判定离线阈值 (默认 30s)
	BatchConcurrency     int           `mapstructure:"batch_concurrency"`      // 批量操作并发数 (默认 50)
	HTTPClientTimeout    time.Duration `mapstructure:"http_client_timeout"`    // Master 请求 Worker 的超时
}

// ================= Worker Config =================

type WorkerConfig struct {
	Server  WorkerServerConfig `mapstructure:"server"`
	Connect ConnectConfig      `mapstructure:"connect"`
	Logic   WorkerLogicConfig  `mapstructure:"logic"`
	Log     LogConfig          `mapstructure:"log"`
	Auth    AuthConfig         `mapstructure:"auth"`
}

type WorkerServerConfig struct {
	Port    int    `mapstructure:"port"`
	WorkDir string `mapstructure:"work_dir"`
}

type ConnectConfig struct {
	MasterURL string `mapstructure:"master_url"`
}

type WorkerLogicConfig struct {
	HeartbeatInterval time.Duration `mapstructure:"heartbeat_interval"` // 心跳间隔 (默认 5s)
	MonitorInterval   time.Duration `mapstructure:"monitor_interval"`   // 监控采集间隔 (默认 3s)
	HTTPClientTimeout time.Duration `mapstructure:"http_client_timeout"`
}

// ================= Common =================

type LogConfig struct {
	Level string `mapstructure:"level"` // "debug", "info", "error"
}

type AuthConfig struct {
	SecretKey string `mapstructure:"secret_key"`
}

// GlobalConfig 动态系统配置 (存入 sys_settings 表)
type GlobalConfig struct {
	Logic struct {
		NodeOfflineThreshold int `json:"node_offline_threshold"` // 秒
		HTTPClientTimeout    int `json:"http_client_timeout"`    // 秒
	} `json:"logic"`

	Worker struct {
		HeartbeatInterval int `json:"heartbeat_interval"` // 秒
		MonitorInterval   int `json:"monitor_interval"`   // 秒
	} `json:"worker"`

	Log struct {
		RetentionDays int `json:"retention_days"` // 保留天数
	} `json:"log"`
}

// DefaultGlobalConfig 获取默认配置
func DefaultGlobalConfig() GlobalConfig {
	var c GlobalConfig
	c.Logic.NodeOfflineThreshold = 30
	c.Logic.HTTPClientTimeout = 5
	c.Worker.HeartbeatInterval = 5
	c.Worker.MonitorInterval = 3
	c.Log.RetentionDays = 180
	return c
}
