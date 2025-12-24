package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"ops-system/pkg/packer"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx context.Context
}

func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// SelectDir 选择目录
func (a *App) SelectDir() string {
	dir, _ := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "选择服务包源码目录",
	})
	return dir
}

// SelectSaveFile 选择保存路径
func (a *App) SelectSaveFile(defaultName string) string {
	file, _ := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "保存压缩包",
		DefaultFilename: defaultName,
		Filters:         []runtime.FileFilter{{DisplayName: "Zip Files (*.zip)", Pattern: "*.zip"}},
	})
	return file
}

// LoadManifest 读取目录下的 service.json
func (a *App) LoadManifest(dir string) (string, error) {
	path := filepath.Join(dir, "service.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// 如果不存在，尝试生成一个默认模板并返回，但不写入磁盘
		// 这里为了简单，返回空字符串，前端判断为空则初始化默认值
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SaveManifest 保存配置到 service.json
func (a *App) SaveManifest(dir string, jsonStr string) error {
	// 校验 JSON 格式
	var tmp interface{}
	if err := json.Unmarshal([]byte(jsonStr), &tmp); err != nil {
		return fmt.Errorf("invalid json format")
	}

	path := filepath.Join(dir, "service.json")
	return os.WriteFile(path, []byte(jsonStr), 0644)
}

// InitTemplate 初始化 (如果文件不存在，写入默认模板)
func (a *App) InitTemplate(dir string) error {
	return packer.GenerateTemplate(dir)
}

// BuildPackage 打包
func (a *App) BuildPackage(srcDir string, destFile string) error {
	return packer.Pack(srcDir, destFile)
}
