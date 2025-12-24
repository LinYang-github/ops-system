package manager

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ops-system/pkg/protocol"
	"ops-system/pkg/storage" // 引入新包
)

type PackageManager struct {
	store storage.Provider // 使用接口
}

func NewPackageManager(store storage.Provider) *PackageManager {
	return &PackageManager{store: store}
}

// SavePackageStream 核心逻辑变更
func (pm *PackageManager) SavePackageStream(reader io.Reader, originalFilename string) (*protocol.ServiceManifest, error) {
	// 1. 无论是 Local 还是 MinIO，我们都需要先在 Master 本地落地成临时文件
	// 因为我们需要随机读取 ZIP 来解析 service.json，而 MinIO 的流不支持 Seek
	tempFile, err := os.CreateTemp("", "upload-*.zip")
	if err != nil {
		return nil, err
	}
	tempPath := tempFile.Name()
	defer func() {
		tempFile.Close()
		os.Remove(tempPath) // 处理完删掉
	}()

	// 2. 写入本地临时文件
	if _, err := io.Copy(tempFile, reader); err != nil {
		return nil, err
	}

	// 3. 解析 ZIP (校验 manifest)
	// 重新打开用于 ReadAt
	zipReader, err := zip.OpenReader(tempPath)
	if err != nil {
		return nil, fmt.Errorf("invalid zip: %v", err)
	}

	var manifest *protocol.ServiceManifest
	for _, f := range zipReader.File {
		if strings.EqualFold(f.Name, "service.json") {
			rc, _ := f.Open()
			manifest = &protocol.ServiceManifest{}
			json.NewDecoder(rc).Decode(manifest)
			rc.Close()
			break
		}
	}
	zipReader.Close() // 关掉以便后续操作

	if manifest == nil {
		return nil, fmt.Errorf("service.json missing")
	}

	// 4. 保存到 Storage (Local 或 MinIO)
	// Key 格式: serviceName/version.zip
	objectKey := filepath.Join(manifest.Name, fmt.Sprintf("%s.zip", manifest.Version))

	// 重新打开临时文件读取流
	uploadFile, _ := os.Open(tempPath)
	defer uploadFile.Close()

	if err := pm.store.Save(objectKey, uploadFile); err != nil {
		return nil, fmt.Errorf("storage save failed: %v", err)
	}

	return manifest, nil
}

// ListPackages
func (pm *PackageManager) ListPackages() ([]protocol.PackageInfo, error) {
	files, err := pm.store.ListFiles()
	if err != nil {
		return nil, err
	}

	// 转换逻辑：把 flat 的文件列表聚合成 Service -> Versions
	pkgMap := make(map[string]*protocol.PackageInfo)

	for _, f := range files {
		// 期望格式: serviceName/version.zip
		// 或者是 windows: serviceName\version.zip
		cleanName := strings.ReplaceAll(f.Name, "\\", "/")
		parts := strings.Split(cleanName, "/")
		if len(parts) != 2 {
			continue
		}

		svcName := parts[0]
		ver := strings.TrimSuffix(parts[1], ".zip")

		if _, ok := pkgMap[svcName]; !ok {
			pkgMap[svcName] = &protocol.PackageInfo{Name: svcName, Versions: []string{}}
		}
		pkgMap[svcName].Versions = append(pkgMap[svcName].Versions, ver)
		if f.ModTime > pkgMap[svcName].LastUpload {
			pkgMap[svcName].LastUpload = f.ModTime
		}
	}

	var list []protocol.PackageInfo
	for _, v := range pkgMap {
		sort.Strings(v.Versions)
		list = append(list, *v)
	}
	return list, nil
}

func (pm *PackageManager) DeletePackage(name, version string) error {
	key := filepath.Join(name, fmt.Sprintf("%s.zip", version))
	return pm.store.Delete(key)
}

// GetManifest
func (pm *PackageManager) GetManifest(name, version string) (*protocol.ServiceManifest, error) {
	key := filepath.Join(name, fmt.Sprintf("%s.zip", version))

	// 从存储获取流
	rc, err := pm.store.Get(key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	// 这里有个问题：zip.NewReader 需要 ReaderAt (随机读取)
	// MinIO 流不支持。所以必须先下载到本地临时文件。
	// 这在 GetManifest 中是不可避免的开销。

	tmp, _ := os.CreateTemp("", "manifest-*.zip")
	defer os.Remove(tmp.Name())

	io.Copy(tmp, rc)

	zipR, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return nil, err
	}
	defer zipR.Close()

	for _, f := range zipR.File {
		if strings.EqualFold(f.Name, "service.json") {
			m := &protocol.ServiceManifest{}
			r, _ := f.Open()
			json.NewDecoder(r).Decode(m)
			r.Close()
			return m, nil
		}
	}
	return nil, fmt.Errorf("not found")
}

// 【新增】暴露获取 URL 的方法供 Handler 使用
func (pm *PackageManager) GetDownloadURL(name, version, masterAddr string) (string, error) {
	key := filepath.Join(name, fmt.Sprintf("%s.zip", version))
	return pm.store.GetDownloadURL(key, masterAddr)
}
