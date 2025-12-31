package executor

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"ops-system/pkg/protocol"
)

// CleanResult 清理结果统计
type CleanResult struct {
	FreedBytes   int64    `json:"freed_bytes"`
	DeletedFiles []string `json:"deleted_files"`
	Errors       []string `json:"errors"`
}

// CleanupPackageCache 清理下载缓存
func (m *Manager) CleanupPackageCache(retainCount int) (CleanResult, error) {
	result := CleanResult{
		DeletedFiles: []string{},
		Errors:       []string{},
	}

	if m.pkgCacheDir == "" {
		return result, fmt.Errorf("pkg_cache dir not initialized")
	}

	entries, err := os.ReadDir(m.pkgCacheDir)
	if err != nil {
		return result, err
	}

	// 1. 分组
	groups := make(map[string][]os.DirEntry)

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".zip") {
			continue
		}

		name := entry.Name()
		nameNoExt := strings.TrimSuffix(name, ".zip")

		// 简单的分割策略
		idx := strings.LastIndex(nameNoExt, "_")
		serviceName := ""
		if idx == -1 {
			serviceName = nameNoExt
		} else {
			serviceName = nameNoExt[:idx]
		}

		groups[serviceName] = append(groups[serviceName], entry)
	}

	// 2. 遍历分组进行清理
	for svcName, files := range groups {
		if retainCount < 0 || len(files) <= retainCount {
			continue
		}

		// 按修改时间倒序排列 (最新的在前)
		sort.Slice(files, func(i, j int) bool {
			iInfo, _ := files[i].Info()
			jInfo, _ := files[j].Info()
			return iInfo.ModTime().After(jInfo.ModTime())
		})

		for i := retainCount; i < len(files); i++ {
			fileEntry := files[i]
			fullPath := filepath.Join(m.pkgCacheDir, fileEntry.Name())

			info, err := fileEntry.Info()
			size := int64(0)
			if err == nil {
				size = info.Size()
			}

			if err := os.Remove(fullPath); err != nil {
				errMsg := fmt.Sprintf("Failed to delete %s: %v", fileEntry.Name(), err)
				log.Printf("[Cleaner] %s", errMsg)
				result.Errors = append(result.Errors, errMsg)
			} else {
				log.Printf("[Cleaner] Deleted cache: %s (%d bytes)", fileEntry.Name(), size)
				result.FreedBytes += size
				result.DeletedFiles = append(result.DeletedFiles, fileEntry.Name())
			}
		}

		if retainCount > 0 {
			log.Printf("[Cleaner] Service [%s]: Kept %d latest, deleted %d files.", svcName, retainCount, len(files)-retainCount)
		}
	}

	return result, nil
}

// ScanOrphans 扫描孤儿资源
func (m *Manager) ScanOrphans(validSysMap map[string]bool, validInstMap map[string]bool) ([]protocol.OrphanItem, error) {
	var orphans []protocol.OrphanItem

	if m.workDir == "" {
		return nil, fmt.Errorf("executor not initialized")
	}

	// 1. 读取 instances 下的一级目录 (System 层)
	sysEntries, err := os.ReadDir(m.workDir)
	if err != nil {
		return nil, err
	}

	for _, sysEntry := range sysEntries {
		if !sysEntry.IsDir() {
			continue
		}
		sysName := sysEntry.Name()

		if sysName == "pkg_cache" || sysName == "external" || sysName == "lost+found" {
			continue
		}

		if !validSysMap[sysName] {
			// 整个系统都是孤儿
			sysPath := filepath.Join(m.workDir, sysName)
			size := getDirSize(sysPath)
			orphans = append(orphans, protocol.OrphanItem{
				Type:    "system_dir",
				Path:    sysName,
				AbsPath: sysPath,
				Size:    size,
			})
			continue
		}

		// 2. 如果系统合法，扫描二级目录
		sysPath := filepath.Join(m.workDir, sysName)
		instEntries, err := os.ReadDir(sysPath)
		if err != nil {
			continue
		}

		for _, instEntry := range instEntries {
			if !instEntry.IsDir() {
				continue
			}
			dirName := instEntry.Name()

			instID := dirName
			idx := strings.LastIndex(dirName, "_")
			if idx != -1 {
				instID = dirName[idx+1:]
			}

			if !validInstMap[instID] {
				fullPath := filepath.Join(sysPath, dirName)
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
func (m *Manager) DeleteOrphans(relPaths []string) (int, error) {
	count := 0
	for _, relPath := range relPaths {
		if strings.Contains(relPath, "..") {
			continue
		}

		fullPath := filepath.Join(m.workDir, relPath)

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

// getDirSize 辅助函数 (保持不变，因为是无状态的)
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
