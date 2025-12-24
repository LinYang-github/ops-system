package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/response"
)

// UploadPackage 处理上传 (Stream 模式，支持大文件)
// POST /api/upload
func (h *ServerHandler) UploadPackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	// 1. 获取 Multipart Reader (不预加载到内存)
	reader, err := r.MultipartReader()
	if err != nil {
		response.Error(w, e.New(code.PackageUploadFailed, "无法解析上传请求", err))
		return
	}

	// 2. 遍历 Part 寻找 file 字段
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			response.Error(w, e.New(code.PackageUploadFailed, "读取上传流中断", err))
			return
		}

		// 找到名为 "file" 的表单项
		if part.FormName() == "file" {
			// 3. 将流直接传给 Manager
			manifest, err := h.pkgMgr.SavePackageStream(part, part.FileName())
			if err != nil {
				// 这里根据错误内容可以细分，暂统一为 UploadFailed
				response.Error(w, e.New(code.PackageUploadFailed, fmt.Sprintf("处理服务包失败: %v", err), err))
				return
			}

			// 4. 成功响应
			// 返回 manifest 信息方便前端展示
			response.Success(w, map[string]interface{}{
				"service": manifest.Name,
				"version": manifest.Version,
				"os":      manifest.OS,
			})
			return
		}
	}

	// 如果循环结束还没找到 file 字段
	response.Error(w, e.New(code.ParamError, "未找到 file 表单字段", nil))
}

// ListPackages 获取包列表
func (h *ServerHandler) ListPackages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	list, err := h.pkgMgr.ListPackages()
	if err != nil {
		response.Error(w, e.New(code.ServerError, "获取列表失败", err))
		return
	}
	response.Success(w, list)
}

// DeletePackage 删除包
func (h *ServerHandler) DeletePackage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	type DeleteReq struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	var req DeleteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if err := h.pkgMgr.DeletePackage(req.Name, req.Version); err != nil {
		response.Error(w, e.New(code.PackageDeleteFailed, "删除文件失败", err))
		return
	}

	response.Success(w, nil)
}

// GetPackageManifest 获取包配置详情
func (h *ServerHandler) GetPackageManifest(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	name := query.Get("name")
	version := query.Get("version")

	if name == "" || version == "" {
		response.Error(w, e.New(code.ParamError, "缺少 name 或 version 参数", nil))
		return
	}

	manifest, err := h.pkgMgr.GetManifest(name, version)
	if err != nil {
		response.Error(w, e.New(code.PackageNotFound, "读取配置失败", err))
		return
	}

	response.Success(w, manifest)
}
