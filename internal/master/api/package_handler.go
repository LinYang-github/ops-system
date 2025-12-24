package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// handleUploadPackage 处理上传 (Stream 模式，支持大文件)
func (h *ServerHandler) UploadPackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}

	// 1. 获取 Multipart Reader (不预加载到内存)
	reader, err := r.MultipartReader()
	if err != nil {
		http.Error(w, "Invalid multipart request", 400)
		return
	}

	// 2. 遍历 Part 寻找 file 字段
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			http.Error(w, "Read upload stream failed", 500)
			return
		}

		// 找到名为 "file" 的表单项
		if part.FormName() == "file" {
			// 3. 将流直接传给 Manager
			manifest, err := h.pkgMgr.SavePackageStream(part, part.FileName())
			if err != nil {
				http.Error(w, fmt.Sprintf("Process failed: %v", err), 400)
				return
			}

			// 成功，返回 JSON
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"msg":     "Upload success",
				"service": manifest.Name,
				"version": manifest.Version,
			})
			return
		}
	}

	http.Error(w, "No file part found", 400)
}

// handleListPackages 获取包列表
func (h *ServerHandler) ListPackages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	list, err := h.pkgMgr.ListPackages()
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(list)
}

// handleDeletePackage 删除包
func (h *ServerHandler) DeletePackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", 405)
		return
	}
	type DeleteReq struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	var req DeleteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", 400)
		return
	}
	if err := h.pkgMgr.DeletePackage(req.Name, req.Version); err != nil {
		http.Error(w, fmt.Sprintf("Delete failed: %v", err), 500)
		return
	}
	w.Write([]byte(`{"status":"ok"}`))
}

// handleGetPackageManifest 获取包配置详情 (修复报错的核心函数)
// URL: /api/packages/manifest?name=...&version=...
func (h *ServerHandler) GetPackageManifest(w http.ResponseWriter, r *http.Request) {
	// 1. 获取参数
	query := r.URL.Query()
	name := query.Get("name")
	version := query.Get("version")

	if name == "" || version == "" {
		http.Error(w, "missing name or version", 400)
		return
	}

	// 2. 调用 Manager 读取
	manifest, err := h.pkgMgr.GetManifest(name, version)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}

	// 3. 返回 JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(manifest)
}
