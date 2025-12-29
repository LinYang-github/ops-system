package manager

import (
	"archive/zip"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"ops-system/pkg/protocol"
	"ops-system/pkg/storage"
)

type PackageManager struct {
	db    *sql.DB // 引入 DB
	store storage.Provider
}

// 修改构造函数，注入 DB
func NewPackageManager(db *sql.DB, store storage.Provider) *PackageManager {
	return &PackageManager{
		db:    db,
		store: store,
	}
}

// SavePackageStream 上传流处理：解析 -> 存文件 -> 存DB
func (pm *PackageManager) SavePackageStream(reader io.Reader, originalFilename string) (*protocol.ServiceManifest, error) {
	// 1. 落地临时文件 (为了解压解析)
	tempFile, err := os.CreateTemp("", "upload-*.zip")
	if err != nil {
		return nil, err
	}
	tempPath := tempFile.Name()
	defer func() {
		tempFile.Close()
		os.Remove(tempPath)
	}()

	size, err := io.Copy(tempFile, reader)
	if err != nil {
		return nil, err
	}

	// 2. 解析 ZIP，提取 service.json
	zipReader, err := zip.OpenReader(tempPath)
	if err != nil {
		return nil, fmt.Errorf("invalid zip: %v", err)
	}

	var manifest *protocol.ServiceManifest
	var manifestBytes []byte // 保存原始 JSON 字节

	for _, f := range zipReader.File {
		if f.Name == "service.json" { // 严格匹配
			rc, _ := f.Open()
			manifestBytes, _ = io.ReadAll(rc)
			rc.Close()

			manifest = &protocol.ServiceManifest{}
			if err := json.Unmarshal(manifestBytes, manifest); err != nil {
				zipReader.Close()
				return nil, fmt.Errorf("invalid service.json: %v", err)
			}
			break
		}
	}
	zipReader.Close()

	if manifest == nil {
		return nil, fmt.Errorf("service.json not found")
	}
	if manifest.Name == "" || manifest.Version == "" {
		return nil, fmt.Errorf("missing name or version")
	}

	// 3. 保存实体文件到 Storage (Local/MinIO)
	objectKey := filepath.Join(manifest.Name, fmt.Sprintf("%s.zip", manifest.Version))

	// 重新打开临时文件读取
	uploadFile, _ := os.Open(tempPath)
	defer uploadFile.Close()

	if err := pm.store.Save(objectKey, uploadFile); err != nil {
		return nil, fmt.Errorf("storage save failed: %v", err)
	}

	// 4. 【核心优化】保存元数据到 SQLite
	// 使用 REPLACE INTO 支持覆盖上传
	_, err = pm.db.Exec(`
		INSERT OR REPLACE INTO package_infos (name, version, size, upload_time, manifest)
		VALUES (?, ?, ?, ?, ?)
	`, manifest.Name, manifest.Version, size, time.Now().Unix(), string(manifestBytes))

	if err != nil {
		// 如果写库失败，尝试回滚删除文件（可选）
		pm.store.Delete(objectKey)
		return nil, fmt.Errorf("save db failed: %v", err)
	}

	return manifest, nil
}

// ListPackages 获取包列表 (纯 DB 查询，极速)
func (pm *PackageManager) ListPackages() ([]protocol.PackageInfo, error) {
	rows, err := pm.db.Query(`SELECT name, version, upload_time FROM package_infos ORDER BY upload_time DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// 聚合逻辑：Service -> Versions
	pkgMap := make(map[string]*protocol.PackageInfo)

	for rows.Next() {
		var name, version string
		var uploadTime int64
		if err := rows.Scan(&name, &version, &uploadTime); err != nil {
			continue
		}

		if _, ok := pkgMap[name]; !ok {
			pkgMap[name] = &protocol.PackageInfo{Name: name, Versions: []string{}}
		}
		pkgMap[name].Versions = append(pkgMap[name].Versions, version)
		// 记录最新的时间
		if uploadTime > pkgMap[name].LastUpload {
			pkgMap[name].LastUpload = uploadTime
		}
	}

	var list []protocol.PackageInfo
	for _, v := range pkgMap {
		list = append(list, *v)
	}
	return list, nil
}

// DeletePackage 删除包 (删 DB + 删文件)
func (pm *PackageManager) DeletePackage(name, version string) error {
	// 1. 删文件
	key := filepath.Join(name, fmt.Sprintf("%s.zip", version))
	if err := pm.store.Delete(key); err != nil {
		// 如果文件本身就不存在，忽略错误，继续删 DB
		// return err
	}

	// 2. 删 DB
	_, err := pm.db.Exec("DELETE FROM package_infos WHERE name = ? AND version = ?", name, version)
	return err
}

// GetManifest 获取详情 (纯 DB 查询，零 IO)
func (pm *PackageManager) GetManifest(name, version string) (*protocol.ServiceManifest, error) {
	var manifestJSON string
	err := pm.db.QueryRow("SELECT manifest FROM package_infos WHERE name = ? AND version = ?", name, version).Scan(&manifestJSON)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("package not found in db")
	}
	if err != nil {
		return nil, err
	}

	var m protocol.ServiceManifest
	if err := json.Unmarshal([]byte(manifestJSON), &m); err != nil {
		return nil, fmt.Errorf("parse manifest failed")
	}
	return &m, nil
}

// GetDownloadURL 获取下载链接 (直通 Storage)
func (pm *PackageManager) GetDownloadURL(name, version, masterAddr string) (string, error) {
	key := filepath.Join(name, fmt.Sprintf("%s.zip", version))
	return pm.store.GetDownloadURL(key, masterAddr)
}

// GetUploadURL 获取直传链接
func (pm *PackageManager) GetUploadURL(filename string, expire time.Duration) (string, error) {
	return pm.store.GetUploadURL(filename, expire)
}

// SaveRaw 用于直传回调或本地直传
func (pm *PackageManager) SaveRaw(filename string, reader io.Reader) error {
	return pm.store.Save(filename, reader)
}

// RegisterPackageMetadata 仅注册元数据到数据库 (用于前端直传后的回调)
func (pm *PackageManager) RegisterPackageMetadata(manifest *protocol.ServiceManifest, size int64) error {
	pm.db = pm.db // 确保 db 不为空

	// 序列化 manifest 用于存储
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal manifest failed: %v", err)
	}

	// 写入数据库
	// 使用 REPLACE INTO，如果版本已存在则更新
	_, err = pm.db.Exec(`
		INSERT OR REPLACE INTO package_infos (name, version, size, upload_time, manifest)
		VALUES (?, ?, ?, ?, ?)
	`, manifest.Name, manifest.Version, size, time.Now().Unix(), string(manifestBytes))

	if err != nil {
		return fmt.Errorf("save db failed: %v", err)
	}

	return nil
}
