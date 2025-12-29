package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// LoadMasterConfig 加载 Master 配置
func LoadMasterConfig(cfgFile string) (*MasterConfig, error) {
	// 1. 获取全局 Viper 实例 (这就包含了 main.go 里绑定的命令行参数)
	v := viper.GetViper()

	// 2. 设置兜底默认值 (仅设置那些命令行里没计算过的)
	// 注意：像 port, upload_dir 这种在 main.go 里通过 pflag 设置了默认值的，这里不需要再设，
	// 否则可能会覆盖掉 pflag 的逻辑。
	v.SetDefault("server.read_timeout", 0)
	v.SetDefault("server.write_timeout", 0)
	v.SetDefault("server.api_timeout", 10) // 默认 10秒超时

	v.SetDefault("storage.type", "local")
	// MinIO 默认值
	v.SetDefault("storage.minio.endpoint", "127.0.0.1:9000")
	v.SetDefault("storage.minio.ak", "minioadmin")
	v.SetDefault("storage.minio.sk", "minioadmin")
	v.SetDefault("storage.minio.bucket", "ops-packages")

	v.SetDefault("logic.node_offline_threshold", "30s")
	v.SetDefault("logic.batch_concurrency", 50)
	v.SetDefault("logic.http_client_timeout", "5s")
	v.SetDefault("auth.secret_key", "ops-system-secret-key")

	// 3. 绑定环境变量
	v.SetEnvPrefix("OPS_MASTER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// 4. 读取配置文件 (如果有)
	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
		if err := v.ReadInConfig(); err != nil {
			if !os.IsNotExist(err) {
				return nil, fmt.Errorf("read config file failed: %v", err)
			}
			fmt.Printf("Config file specified but not found: %s (using flags/defaults)\n", cfgFile)
		} else {
			fmt.Printf("Loaded config from: %s\n", v.ConfigFileUsed())
		}
	} else {
		// 尝试默认路径 (可选)
		v.AddConfigPath(".")
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err == nil {
			fmt.Printf("Loaded default config: %s\n", v.ConfigFileUsed())
		}
	}

	var c MasterConfig
	if err := v.Unmarshal(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

// LoadWorkerConfig 加载 Worker 配置
func LoadWorkerConfig(cfgFile string) (*WorkerConfig, error) {
	// Worker 同理，使用全局实例
	v := viper.GetViper()

	v.SetDefault("server.port", 8081)
	v.SetDefault("logic.heartbeat_interval", "5s")
	v.SetDefault("logic.monitor_interval", "3s")
	v.SetDefault("logic.http_client_timeout", "10s")
	v.SetDefault("auth.secret_key", "ops-system-secret-key")

	v.SetEnvPrefix("OPS_WORKER")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if cfgFile != "" {
		v.SetConfigFile(cfgFile)
		if err := v.ReadInConfig(); err != nil {
			if !os.IsNotExist(err) {
				return nil, err
			}
		}
	} else {
		v.AddConfigPath(".")
		v.SetConfigName("worker")
		v.SetConfigType("yaml")
		v.ReadInConfig()
	}

	var c WorkerConfig
	if err := v.Unmarshal(&c); err != nil {
		return nil, err
	}
	return &c, nil
}
