package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/response"
	"ops-system/pkg/utils"
)

// 定义允许管理的系统二进制文件列表 (白名单)
var allowedArtifacts = map[string]string{
	"linux_amd64":   "worker_linux_amd64",
	"windows_amd64": "worker_windows_amd64.exe",
	// "linux_arm64":   "worker_linux_arm64", // 可扩展
}

type ArtifactInfo struct {
	Key         string `json:"key"`          // 标识符 (linux_amd64)
	Filename    string `json:"filename"`     // 文件名
	Exists      bool   `json:"exists"`       // 是否存在
	Size        int64  `json:"size"`         // 大小
	ModTime     int64  `json:"mod_time"`     // 修改时间
	DownloadURL string `json:"download_url"` // 下载链接
}

// ListSystemArtifacts 获取系统制品列表
// GET /api/maintenance/artifacts
func (h *ServerHandler) ListSystemArtifacts(w http.ResponseWriter, r *http.Request) {
	baseDir := filepath.Join(h.uploadDir, "system")
	os.MkdirAll(baseDir, 0755) // 确保目录存在

	var list []ArtifactInfo
	host := r.Host

	for key, filename := range allowedArtifacts {
		path := filepath.Join(baseDir, filename)
		info := ArtifactInfo{
			Key:         key,
			Filename:    filename,
			Exists:      false,
			DownloadURL: fmt.Sprintf("http://%s/download/system/%s", host, filename),
		}

		if stat, err := os.Stat(path); err == nil && !stat.IsDir() {
			info.Exists = true
			info.Size = stat.Size()
			info.ModTime = stat.ModTime().Unix()
		}

		list = append(list, info)
	}

	response.Success(w, list)
}

// UploadSystemArtifact 上传系统制品
// POST /api/maintenance/artifacts/upload
func (h *ServerHandler) UploadSystemArtifact(w http.ResponseWriter, r *http.Request) {
	// 1. 解析 Form
	// key: 标识符 (如 linux_amd64)
	// file: 文件流
	key := r.FormValue("key")
	targetFilename, ok := allowedArtifacts[key]
	if !ok {
		response.Error(w, e.New(code.ParamError, "无效的制品标识符", nil))
		return
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		response.Error(w, e.New(code.ParamError, "读取文件失败", err))
		return
	}
	defer file.Close()

	// 2. 准备路径
	baseDir := filepath.Join(h.uploadDir, "system")
	os.MkdirAll(baseDir, 0755)
	destPath := filepath.Join(baseDir, targetFilename)

	// 3. 写入文件 (覆盖)
	out, err := os.Create(destPath)
	if err != nil {
		response.Error(w, e.New(code.ServerError, "创建文件失败", err))
		return
	}
	defer out.Close()

	size, err := io.Copy(out, file)
	if err != nil {
		response.Error(w, e.New(code.ServerError, "写入文件失败", err))
		return
	}

	h.logMgr.RecordLog(utils.GetClientIP(r), "upload_artifact", "system", key, fmt.Sprintf("Size: %d", size), "success")
	response.Success(w, nil)
}
