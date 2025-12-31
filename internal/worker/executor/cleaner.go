package executor

import (
	"fmt"
	"log"
	"ops-system/pkg/protocol"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CleanResult 清理结果统计
type CleanResult struct {
	FreedBytes   int64    `json:"freed_bytes"`
	DeletedFiles []string `json:"deleted_files"`
	Errors       []string `json:"errors"`
}

// CleanupPackageCache 清理下载缓存
// retainCount: 每个服务保留的最近版本数量 (0 表示全部清理，-1 表示不清理仅统计)
func CleanupPackageCache(retainCount int) (CleanResult, error) {
	result := CleanResult{
		DeletedFiles: []string{},
		Errors:       []string{},
	}

	if pkgCacheDir == "" {
		return result, fmt.Errorf("pkg_cache dir not initialized")
	}

	entries, err := os.ReadDir(pkgCacheDir)
	if err != nil {
		return result, err
	}

	// 1. 分组：Key = ServiceName, Value = []FileInfo
	// 假设文件名格式为: ServiceName_Version.zip
	// 使用 LastIndex("_") 作为分割点的启发式算法
	groups := make(map[string][]os.DirEntry)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}

		name := entry.Name()
		nameNoExt := strings.TrimSuffix(name, ".zip")

		// 尝试分割服务名和版本号
		// 策略：取最后一个 "_" 之前的部分作为服务名
		// e.g. "my_app_v1.0.zip" -> Service: "my_app", Version: "v1.0"
		idx := strings.LastIndex(nameNoExt, "_")
		serviceName := ""
		if idx == -1 {
			// 如果没有下划线，整个文件名作为服务名 (虽然不符合生成规则，但为了安全起见归为一类)
			serviceName = nameNoExt
		} else {
			serviceName = nameNoExt[:idx]
		}

		groups[serviceName] = append(groups[serviceName], entry)
	}

	// 2. 遍历分组进行清理
	for svcName, files := range groups {
		// 如果保留数 >= 文件数，跳过
		if retainCount < 0 || len(files) <= retainCount {
			continue
		}

		// 按修改时间倒序排列 (最新的在前)
		sort.Slice(files, func(i, j int) bool {
			iInfo, _ := files[i].Info()
			jInfo, _ := files[j].Info()
			// ModTime 越大越新
			return iInfo.ModTime().After(jInfo.ModTime())
		})

		// 删除多余的文件 (从 retainCount 下标开始)
		for i := retainCount; i < len(files); i++ {
			fileEntry := files[i]
			fullPath := filepath.Join(pkgCacheDir, fileEntry.Name())

			// 获取文件大小用于统计
			info, err := fileEntry.Info()
			size := int64(0)
			if err == nil {
				size = info.Size()
			}

			// 执行删除
			if err := os.Remove(fullPath); err != nil {
				errMsg := fmt.Sprintf("Failed to delete %s: %v", fileEntry.Name(), err)
				log.Printf("[Cleaner] %s", errMsg)
				result.Errors = append(result.Errors, errMsg)
			} else {
				log.Printf("[Cleaner] Deleted cache: %s (%d bytes)", fileEntry.Name(), size)
				result.FreedBytes += size
				// 记录删除的文件名 (为了安全，不记录完整路径)
				result.DeletedFiles = append(result.DeletedFiles, fileEntry.Name())
			}
		}

		if retainCount > 0 {
			log.Printf("[Cleaner] Service [%s]: Kept %d latest versions, deleted %d files.", svcName, retainCount, len(files)-retainCount)
		}
	}

	return result, nil
}

// ScanOrphans 扫描孤儿资源
// validSysMap: Key=SystemName
// validInstMap: Key=InstanceID
func ScanOrphans(validSysMap map[string]bool, validInstMap map[string]bool) ([]protocol.OrphanItem, error) {
	var orphans []protocol.OrphanItem

	if baseWorkDir == "" {
		return nil, fmt.Errorf("executor not initialized")
	}

	// 1. 读取 instances 下的一级目录 (System 层)
	sysEntries, err := os.ReadDir(baseWorkDir)
	if err != nil {
		return nil, err
	}

	for _, sysEntry := range sysEntries {
		if !sysEntry.IsDir() {
			continue
		}
		sysName := sysEntry.Name()

		// 排除保留目录
		if sysName == "pkg_cache" || sysName == "external" || sysName == "lost+found" {
			continue
		}

		// 检查系统是否合法
		if !validSysMap[sysName] {
			// 整个系统都是孤儿
			size := getDirSize(filepath.Join(baseWorkDir, sysName))
			orphans = append(orphans, protocol.OrphanItem{
				Type:    "system_dir",
				Path:    sysName,
				AbsPath: filepath.Join(baseWorkDir, sysName),
				Size:    size,
			})
			continue // 系统都不合法，下面的实例肯定也不合法，直接跳过子目录扫描
		}

		// 2. 如果系统合法，深入扫描二级目录 (Instance 层)
		sysPath := filepath.Join(baseWorkDir, sysName)
		instEntries, err := os.ReadDir(sysPath)
		if err != nil {
			continue
		}

		for _, instEntry := range instEntries {
			if !instEntry.IsDir() {
				continue
			}
			dirName := instEntry.Name() // e.g. "app_inst-123"

			// 解析 InstanceID (取最后一个 "_" 之后的部分)
			// 约定：目录名格式为 ServiceName_InstanceID
			instID := dirName
			idx := strings.LastIndex(dirName, "_")
			if idx != -1 {
				instID = dirName[idx+1:]
			}

			// 检查实例是否合法
			if !validInstMap[instID] {
				fullPath := filepath.Join(sysPath, dirName)

				// 【安全检查】进程是否活着
				running := isRunning(fullPath)
				pid := 0
				if running {
					pid = getPID(fullPath)
				}

				orphans = append(orphans, protocol.OrphanItem{
					Type:      "instance_dir",
					Path:      filepath.Join(sysName, dirName),
					AbsPath:   fullPath,
					Size:      getDirSize(fullPath),
					IsRunning: running,
					Pid:       pid,
				})
			}
		}
	}

	return orphans, nil
}

// DeleteOrphans 删除指定的目录
func DeleteOrphans(relPaths []string) (int, error) {
	count := 0
	for _, relPath := range relPaths {
		// 防御：禁止跳出 baseWorkDir
		if strings.Contains(relPath, "..") {
			continue
		}

		fullPath := filepath.Join(baseWorkDir, relPath)

		// 防御：再次检查是否运行中
		if isRunning(fullPath) {
			log.Printf("[Cleaner] Skip deletion, process is running: %s", fullPath)
			continue
		}

		log.Printf("[Cleaner] Removing orphan: %s", fullPath)
		if err := os.RemoveAll(fullPath); err == nil {
			count++
		} else {
			log.Printf("[Cleaner] Delete failed: %v", err)
		}
	}
	return count, nil
}

// getDirSize 递归计算目录大小
func getDirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}
