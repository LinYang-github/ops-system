package storage

import (
	"io"
)

type Provider interface {
	// 保存文件 (filename: 如 demo-app/1.0.0.zip)
	Save(filename string, data io.Reader) error

	// 获取文件流 (用于 Master 读取 manifest)
	Get(filename string) (io.ReadCloser, error)

	// 删除文件
	Delete(filename string) error

	// 获取下载链接 (Local返回相对路径, MinIO返回预签名URL)
	// masterAddr: 本地模式需要知道 Master 的 IP:Port
	GetDownloadURL(filename string, masterAddr string) (string, error)

	// 列出所有文件 (用于 ListPackages)
	ListFiles() ([]FileInfo, error)
}

type FileInfo struct {
	Name    string
	Size    int64
	ModTime int64
}
