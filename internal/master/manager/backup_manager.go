package manager

import (
	"archive/zip"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"ops-system/internal/master/db"
	"ops-system/pkg/protocol"
)

type BackupManager struct {
	db        *sql.DB
	BackupDir string // e.g. /app/backups
	DataPath  string // e.g. /app/ops_data.db
	UploadDir string // e.g. /app/uploads
}

func NewBackupManager(database *sql.DB, uploadDir string) *BackupManager {
	ex, _ := os.Executable()
	exPath := filepath.Dir(ex)

	backupDir := filepath.Join(exPath, "backups")
	dataPath := filepath.Join(exPath, "ops_data.db")

	os.MkdirAll(backupDir, 0755)

	return &BackupManager{
		db:        database,
		BackupDir: backupDir,
		DataPath:  dataPath,
		UploadDir: uploadDir,
	}
}

// ListBackups 获取备份列表
func (bm *BackupManager) ListBackups() ([]protocol.BackupFile, error) {
	entries, err := os.ReadDir(bm.BackupDir)
	if err != nil {
		return nil, err
	}

	var list []protocol.BackupFile
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".zip") {
			info, _ := entry.Info()
			// 简单判断：如果文件名包含 "_full_" 认为是全量
			withFiles := strings.Contains(entry.Name(), "_full_")

			list = append(list, protocol.BackupFile{
				Name:       entry.Name(),
				Size:       info.Size(),
				CreateTime: info.ModTime().Unix(),
				WithFiles:  withFiles,
			})
		}
	}

	// 按时间倒序
	sort.Slice(list, func(i, j int) bool {
		return list[i].CreateTime > list[j].CreateTime
	})

	return list, nil
}

// CreateBackup 创建备份
func (bm *BackupManager) CreateBackup(withFiles bool) error {
	timestamp := time.Now().Format("20060102_150405")
	tag := "lite"
	if withFiles {
		tag = "full"
	}
	zipName := fmt.Sprintf("backup_%s_%s.zip", tag, timestamp)
	zipPath := filepath.Join(bm.BackupDir, zipName)

	// 1. 生成数据库快照 (VACUUM INTO) - 线程安全，不阻塞读
	snapshotPath := filepath.Join(bm.BackupDir, "snapshot.db")
	os.Remove(snapshotPath) // 清理旧的

	_, err := bm.db.Exec(fmt.Sprintf("VACUUM INTO '%s'", snapshotPath))
	if err != nil {
		return fmt.Errorf("db snapshot failed: %v", err)
	}
	defer os.Remove(snapshotPath) // 备份完删除快照

	// 2. 创建 ZIP
	outFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer outFile.Close()

	w := zip.NewWriter(outFile)
	defer w.Close()

	// 3. 写入数据库文件
	if err := addFileToZip(w, snapshotPath, "ops_data.db"); err != nil {
		return err
	}

	// 4. (可选) 写入 uploads 目录
	if withFiles {
		if err := addDirToZip(w, bm.UploadDir, "uploads"); err != nil {
			return err
		}
	}

	return nil
}

// DeleteBackup 删除备份
func (bm *BackupManager) DeleteBackup(filename string) error {
	// 简单安全检查，防止路径穿越
	if strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		return fmt.Errorf("invalid filename")
	}
	return os.Remove(filepath.Join(bm.BackupDir, filename))
}

// RestoreBackup 恢复备份 (危险操作！)
func (bm *BackupManager) RestoreBackup(filename string) error {
	srcZip := filepath.Join(bm.BackupDir, filename)
	if _, err := os.Stat(srcZip); err != nil {
		return fmt.Errorf("backup file not found")
	}

	// 1. 关闭数据库连接 (必须，否则 Windows 下无法覆盖文件)
	if err := db.CloseDB(bm.db); err != nil {
		return fmt.Errorf("failed to close db: %v", err)
	}

	// 2. 备份当前数据 (Rollback 机制)
	rollbackName := fmt.Sprintf("%s.rollback.%d", bm.DataPath, time.Now().Unix())
	os.Rename(bm.DataPath, rollbackName)

	// 3. 解压覆盖
	if err := unzipRestore(srcZip, filepath.Dir(bm.DataPath)); err != nil {
		// 尝试回滚
		os.Rename(rollbackName, bm.DataPath)
		return fmt.Errorf("restore failed (rolled back): %v", err)
	}

	// 4. 恢复完成，为了重置所有 Manager 的 DB 连接状态，最稳妥的方式是退出进程
	// 让 Supervisor 或用户手动重启
	log.Println(">>> Restore successful. Exiting to reload data...")
	os.Exit(0)

	return nil
}

// --- Helpers ---

func addFileToZip(w *zip.Writer, srcPath, zipPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	writer, err := w.Create(zipPath)
	if err != nil {
		return err
	}

	_, err = io.Copy(writer, src)
	return err
}

func addDirToZip(w *zip.Writer, srcDir, zipBase string) error {
	return filepath.WalkDir(srcDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relPath, _ := filepath.Rel(srcDir, path)
		zipPath := filepath.Join(zipBase, relPath)
		// Windows 路径分隔符处理
		zipPath = strings.ReplaceAll(zipPath, "\\", "/")

		return addFileToZip(w, path, zipPath)
	})
}

func unzipRestore(srcZip, destDir string) error {
	r, err := zip.OpenReader(srcZip)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		// 简单的防路径穿越检查
		if strings.Contains(f.Name, "..") {
			continue
		}

		fpath := filepath.Join(destDir, f.Name)

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
