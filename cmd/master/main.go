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

	flag.Parse()

	// 3. 确保目录存在
	if err := os.MkdirAll(*uploadDir, 0755); err != nil {
		log.Fatalf("Failed to create upload dir: %v", err)
	}

	// 4. 启动
	assets := root.GetAssets()
	if err := api.StartMasterServer(*port, *uploadDir, *dbPath, assets); err != nil {
		log.Fatal(err)
	}
}
