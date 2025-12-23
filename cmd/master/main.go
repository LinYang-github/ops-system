package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	root "ops-system"
	"ops-system/internal/master/api"
)

func main() {
	// 1. 计算默认路径 (基于可执行文件位置)
	ex, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	exPath := filepath.Dir(ex)

	defaultUploadDir := filepath.Join(exPath, "uploads")
	defaultDBPath := filepath.Join(exPath, "ops_data.db")

	// 2. 定义命令行参数
	port := flag.String("port", ":8080", "Listening port (e.g. :8080)")
	uploadDir := flag.String("upload_dir", defaultUploadDir, "Directory to store uploaded packages")
	dbPath := flag.String("db_path", defaultDBPath, "Path to SQLite database file")

	// 新增 MinIO 参数
	storeType := flag.String("store_type", "local", "Storage type: local or minio")
	minioEp := flag.String("minio_endpoint", "127.0.0.1:9000", "MinIO Endpoint")
	minioAk := flag.String("minio_ak", "minioadmin", "MinIO Access Key")
	minioSk := flag.String("minio_sk", "minioadmin", "MinIO Secret Key")
	minioBucket := flag.String("minio_bucket", "ops-packages", "MinIO Bucket")

	flag.Parse()

	// 3. 确保目录存在
	if err := os.MkdirAll(*uploadDir, 0755); err != nil {
		log.Fatalf("Failed to create upload dir: %v", err)
	}

	// 4. 启动
	assets := root.GetAssets()
	// 传入 config 对象
	config := api.ServerConfig{
		Port:      *port,
		UploadDir: *uploadDir,
		DBPath:    *dbPath,
		StoreType: *storeType,
		MinioConfig: api.MinioConfig{
			Endpoint: *minioEp,
			AK:       *minioAk,
			SK:       *minioSk,
			Bucket:   *minioBucket,
		},
	}

	if err := api.StartMasterServer(config, assets); err != nil {
		log.Fatal(err)
	}
}
