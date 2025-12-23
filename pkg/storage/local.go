package storage

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type LocalProvider struct {
	BaseDir string
}

func NewLocalProvider(baseDir string) *LocalProvider {
	os.MkdirAll(baseDir, 0755)
	return &LocalProvider{BaseDir: baseDir}
}

func (l *LocalProvider) Save(filename string, data io.Reader) error {
	fullPath := filepath.Join(l.BaseDir, filename)

	// 确保子目录存在
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return err
	}

	out, err := os.Create(fullPath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, data)
	return err
}

func (l *LocalProvider) Get(filename string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(l.BaseDir, filename))
}

func (l *LocalProvider) Delete(filename string) error {
	return os.Remove(filepath.Join(l.BaseDir, filename))
}

func (l *LocalProvider) GetDownloadURL(filename string, masterAddr string) (string, error) {
	// 返回标准 HTTP 地址: http://master:8080/download/service/version.zip
	// 注意：Windows 下 filename 可能是 backslash，需替换
	webPath := strings.ReplaceAll(filename, "\\", "/")
	return fmt.Sprintf("http://%s/download/%s", masterAddr, webPath), nil
}

func (l *LocalProvider) ListFiles() ([]FileInfo, error) {
	var files []FileInfo
	err := filepath.Walk(l.BaseDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		// 获取相对路径作为文件名
		relPath, _ := filepath.Rel(l.BaseDir, path)
		files = append(files, FileInfo{
			Name:    relPath, // e.g. demo-app/1.0.0.zip
			Size:    info.Size(),
			ModTime: info.ModTime().Unix(),
		})
		return nil
	})
	return files, err
}
