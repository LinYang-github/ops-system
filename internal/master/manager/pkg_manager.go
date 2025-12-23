package manager

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ops-system/pkg/protocol"
)

// PackageManager 负责服务包的存储和索引
type PackageManager struct {
	UploadDir string
}

// NewPackageManager 创建管理器实例
func NewPackageManager(dir string) *PackageManager {
	return &PackageManager{UploadDir: dir}
}

// SavePackageStream 接收流式数据，先落盘再校验
// reader: 上传的数据流
// originalFilename: 用于提取后缀名等信息
func (pm *PackageManager) SavePackageStream(reader io.Reader, originalFilename string) (*protocol.ServiceManifest, error) {
	// 1. 创建临时文件 (放在 uploads 目录下，避免跨分区移动文件导致的性能损耗)
	// pattern: temp-*.upload
	tempFile, err := os.CreateTemp(pm.UploadDir, "temp-*.upload")
	if err != nil {
		return nil, fmt.Errorf("create temp file failed: %v", err)
	}
	tempPath := tempFile.Name()

	// 确保函数退出时，如果未成功，删除临时文件
	success := false
	defer func() {
		tempFile.Close() // 必须先关闭文件句柄
		if !success {
			os.Remove(tempPath)
		}
	}()

	// 2. 流式拷贝：从网络流直接写入硬盘 (内存占用极低，只占 buffer 大小，通常 32KB)
	// 对于 10GB 文件，这里会阻塞直到传输完成
	fileSize, err := io.Copy(tempFile, reader)
	if err != nil {
		return nil, fmt.Errorf("save stream failed: %v", err)
	}

	// 3. 重新打开临时文件用于 ZIP 读取 (zip 需要 ReaderAt)
	// 因为 io.Copy 移动了 offset，且我们需要 ReadAt 能力
	zipFile, err := os.Open(tempPath)
	if err != nil {
		return nil, err
	}
	defer zipFile.Close()

	// 4. 解析 ZIP，寻找 service.json
	zipReader, err := zip.NewReader(zipFile, fileSize)
	if err != nil {
		return nil, fmt.Errorf("invalid zip structure: %v", err)
	}

	var manifest *protocol.ServiceManifest
	for _, f := range zipReader.File {
		if strings.EqualFold(f.Name, "service.json") {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			manifest = &protocol.ServiceManifest{}
			if err := json.NewDecoder(rc).Decode(manifest); err != nil {
				return nil, fmt.Errorf("service.json decode failed: %v", err)
			}
			break
		}
	}

	if manifest == nil {
		return nil, fmt.Errorf("service.json not found in zip")
	}
	if manifest.Name == "" || manifest.Version == "" {
		return nil, fmt.Errorf("manifest missing name or version")
	}

	// 5. 准备正式存储路径
	targetDir := filepath.Join(pm.UploadDir, manifest.Name)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, err
	}

	targetFilename := fmt.Sprintf("%s.zip", manifest.Version)
	targetPath := filepath.Join(targetDir, targetFilename)

	// 6. 移动文件 (Rename 是原子操作，速度极快)
	// 在 Windows 下如果目标文件存在，Rename 可能会失败，所以先 Remove
	if _, err := os.Stat(targetPath); err == nil {
		os.Remove(targetPath)
	}

	// 关闭句柄以释放锁 (重要：Windows 必须先 Close 才能 Rename)
	zipFile.Close()
	tempFile.Close()

	if err := os.Rename(tempPath, targetPath); err != nil {
		// 如果跨分区 Rename 失败，回退到 Copy+Remove (保底逻辑)
		return nil, fmt.Errorf("move file failed: %v", err)
	}

	success = true // 标记成功，defer 不会删除文件

	// 打印日志方便调试
	fmt.Printf("服务包已保存: %s (Size: %.2f GB)\n", targetPath, float64(fileSize)/1024/1024/1024)

	return manifest, nil
}

// SavePackage 处理上传：校验 -> 归档（废弃）
func (pm *PackageManager) SavePackage(fileHeader *multipart.FileHeader) (*protocol.ServiceManifest, error) {
	// 1. 打开上传的文件流
	src, err := fileHeader.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	// 2. 读取文件内容到内存 buffer
	buff := bytes.NewBuffer(nil)
	if _, err := io.Copy(buff, src); err != nil {
		return nil, err
	}
	readerAt := bytes.NewReader(buff.Bytes())

	// 3. 解析 ZIP，寻找 service.json
	zipReader, err := zip.NewReader(readerAt, int64(buff.Len()))
	if err != nil {
		return nil, fmt.Errorf("invalid zip file: %v", err)
	}

	var manifest *protocol.ServiceManifest
	for _, f := range zipReader.File {
		// 寻找 service.json (忽略大小写)
		if strings.EqualFold(f.Name, "service.json") {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			manifest = &protocol.ServiceManifest{}
			if err := json.NewDecoder(rc).Decode(manifest); err != nil {
				return nil, fmt.Errorf("service.json parse error: %v", err)
			}
			break
		}
	}

	if manifest == nil {
		return nil, fmt.Errorf("service.json not found in zip")
	}
	if manifest.Name == "" || manifest.Version == "" {
		return nil, fmt.Errorf("service.json must contain 'name' and 'version'")
	}

	// 4. 准备存储路径: uploads/{ServiceName}/
	targetDir := filepath.Join(pm.UploadDir, manifest.Name)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return nil, err
	}

	// 5. 保存文件: uploads/{ServiceName}/{Version}.zip
	filename := fmt.Sprintf("%s.zip", manifest.Version)
	targetPath := filepath.Join(targetDir, filename)

	// 如果版本已存在，直接覆盖
	dst, err := os.Create(targetPath)
	if err != nil {
		return nil, err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, bytes.NewReader(buff.Bytes())); err != nil {
		return nil, err
	}

	return manifest, nil
}

// ListPackages 扫描目录获取所有包信息
func (pm *PackageManager) ListPackages() ([]protocol.PackageInfo, error) {
	var list []protocol.PackageInfo

	entries, err := os.ReadDir(pm.UploadDir)
	if err != nil {
		// 如果目录不存在，返回空列表
		if os.IsNotExist(err) {
			return list, nil
		}
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		serviceName := entry.Name()
		versions := []string{}
		var lastMod int64 = 0

		// 扫描子目录下的 zip 文件
		subPath := filepath.Join(pm.UploadDir, serviceName)
		files, _ := os.ReadDir(subPath)
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".zip") {
				// 去掉 .zip 后缀作为版本号
				v := strings.TrimSuffix(f.Name(), ".zip")
				versions = append(versions, v)

				// 获取最后修改时间
				if info, err := f.Info(); err == nil {
					if info.ModTime().Unix() > lastMod {
						lastMod = info.ModTime().Unix()
					}
				}
			}
		}

		if len(versions) > 0 {
			// 版本号排序
			sort.Strings(versions)
			list = append(list, protocol.PackageInfo{
				Name:       serviceName,
				Versions:   versions,
				LastUpload: lastMod,
			})
		}
	}
	return list, nil
}

// DeletePackage 删除指定版本
func (pm *PackageManager) DeletePackage(serviceName, version string) error {
	targetPath := filepath.Join(pm.UploadDir, serviceName, fmt.Sprintf("%s.zip", version))
	return os.Remove(targetPath)
}

// GetManifest 读取指定版本的 service.json
func (pm *PackageManager) GetManifest(serviceName, version string) (*protocol.ServiceManifest, error) {
	// 1. 定位 ZIP 文件路径
	zipPath := filepath.Join(pm.UploadDir, serviceName, fmt.Sprintf("%s.zip", version))

	// 2. 打开 ZIP 文件
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("package file not found")
		}
		return nil, err
	}
	defer r.Close()

	// 3. 寻找 service.json
	for _, f := range r.File {
		if strings.EqualFold(f.Name, "service.json") {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()

			var manifest protocol.ServiceManifest
			if err := json.NewDecoder(rc).Decode(&manifest); err != nil {
				return nil, fmt.Errorf("invalid service.json format")
			}
			return &manifest, nil
		}
	}

	return nil, fmt.Errorf("service.json not found in package")
}
