package ops_system

import (
	"embed"
	"io/fs"
)

// 这里假设你在根目录下执行 build，且 web/dist 存在
//go:embed web/dist/*
var webDist embed.FS

// GetAssets 返回处理过的文件系统 (去除 web/dist 前缀)
func GetAssets() fs.FS {
	// 尝试剥离 web/dist 层级，让访问 index.html 变成 /index.html
	sub, err := fs.Sub(webDist, "web/dist")
	if err != nil {
		panic(err)
	}
	return sub
}