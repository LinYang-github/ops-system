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
