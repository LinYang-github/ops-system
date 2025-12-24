package main

import (
	"log"
	"os"
	"path/filepath"

	root "ops-system"
	"ops-system/internal/master/api"
	"ops-system/pkg/config"

	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

func main() {
	// 1. 计算动态默认路径 (基于可执行文件位置)
	ex, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	exPath := filepath.Dir(ex)
	defaultUploadDir := filepath.Join(exPath, "uploads")
	defaultDBPath := filepath.Join(exPath, "ops_data.db")

	// 2. 定义命令行参数 (使用 pflag 替代 flag)
	// -c 或 --config 用于指定配置文件路径
	cfgFile := pflag.StringP("config", "c", "", "Path to config file (optional, e.g. config.yaml)")

	// --- Server 配置 ---
	pflag.String("port", ":8080", "Listening port")
	viper.BindPFlag("server.port", pflag.Lookup("port")) // 绑定到 viper 的 server.port

	pflag.String("db_path", defaultDBPath, "Path to SQLite database file")
	viper.BindPFlag("server.db_path", pflag.Lookup("db_path"))

	// --- Storage 配置 ---
	pflag.String("upload_dir", defaultUploadDir, "Directory to store uploaded packages")
	viper.BindPFlag("storage.upload_dir", pflag.Lookup("upload_dir"))

	pflag.String("store_type", "local", "Storage type: local or minio")
	viper.BindPFlag("storage.type", pflag.Lookup("store_type"))

	// --- MinIO 配置 ---
	pflag.String("minio_endpoint", "127.0.0.1:9000", "MinIO Endpoint")
	viper.BindPFlag("storage.minio.endpoint", pflag.Lookup("minio_endpoint"))

	pflag.String("minio_ak", "minioadmin", "MinIO Access Key")
	viper.BindPFlag("storage.minio.ak", pflag.Lookup("minio_ak"))

	pflag.String("minio_sk", "minioadmin", "MinIO Secret Key")
	viper.BindPFlag("storage.minio.sk", pflag.Lookup("minio_sk"))

	pflag.String("minio_bucket", "ops-packages", "MinIO Bucket")
	viper.BindPFlag("storage.minio.bucket", pflag.Lookup("minio_bucket"))

	// --- Logic 配置 (超时等) ---
	// 即使没有在命令行显式提供 flag，Viper 也会使用 pkg/config/loader.go 中设置的默认值
	// 这里也可以暴露 flag 供覆盖
	pflag.Duration("node_timeout", 0, "Node offline threshold (default 30s)") // 0 表示使用 config 里的默认值
	viper.BindPFlag("logic.node_offline_threshold", pflag.Lookup("node_timeout"))

	// 解析命令行
	pflag.Parse()

	// 3. 加载配置
	// 优先级: Flag > Env > Config File > Default
	cfg, err := config.LoadMasterConfig(*cfgFile)
	if err != nil {
		log.Fatalf("Load config failed: %v", err)
	}

	// 4. 确保上传目录存在 (使用最终生效的配置路径)
	if err := os.MkdirAll(cfg.Storage.UploadDir, 0755); err != nil {
		log.Fatalf("Failed to create upload dir: %v", err)
	}

	// 5. 启动服务
	assets := root.GetAssets()

	// 注意：你需要修改 internal/master/api/server.go 中的 StartMasterServer 签名
	// 让它接收 *config.MasterConfig 类型
	if err := api.StartMasterServer(cfg, assets); err != nil {
		log.Fatal(err)
	}
}
