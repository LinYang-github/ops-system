package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"ops-system/pkg/code"
	"ops-system/pkg/e"
	"ops-system/pkg/protocol"
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

// 1. 预签名接口
// POST /api/package/presign
func (h *ServerHandler) PresignUpload(w http.ResponseWriter, r *http.Request) {
	var manifest protocol.ServiceManifest
	if err := json.NewDecoder(r.Body).Decode(&manifest); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "service.json 格式错误", err))
		return
	}

	// 校验必填项
	if manifest.Name == "" || manifest.Version == "" {
		response.Error(w, e.New(code.ParamError, "缺少 name 或 version", nil))
		return
	}

	// 生成存储 Key
	fileKey := fmt.Sprintf("%s/%s.zip", manifest.Name, manifest.Version)

	// 获取上传链接 (有效期 1 小时)
	// 注意：这里需要处理 StoreType，如果是 Local 模式，应该报错或返回特殊逻辑
	uploadUrl, err := h.pkgMgr.GetUploadURL(fileKey, time.Hour)
	if err != nil {
		response.Error(w, e.New(code.ServerError, "生成上传链接失败 (当前存储模式可能不支持直传)", err))
		return
	}

	response.Success(w, map[string]string{
		"uploadUrl": uploadUrl,
		"fileKey":   fileKey,
	})
}

// 2. 回调接口
// POST /api/package/callback
func (h *ServerHandler) UploadCallback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Key     string `json:"key"` // MinIO 中的 Key
	}
	json.NewDecoder(r.Body).Decode(&req)

	// 此时文件已经在 MinIO 里了
	// 我们需要确认一下文件是否存在 (Stat)，然后更新某些元数据(如果需要)
	// 由于 ListPackages 是实时查 MinIO 的，其实这里不做操作也可以。
	// 但为了严谨，可以检查一下文件是否存在。

	// h.pkgMgr.CheckExists(req.Key) ...

	response.Success(w, nil)
}

// HandleDirectUpload 处理本地模式的直传请求
// PUT /api/upload/direct?key=...
func (h *ServerHandler) HandleDirectUpload(w http.ResponseWriter, r *http.Request) {
	// 1. 【关键修复】处理 CORS 预检请求 (OPTIONS)
	// 允许跨域，因为这是给前端直传用的
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "PUT, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 2. 校验请求方法
	if r.Method != http.MethodPut {
		// 注意：这里不能用 response.Error，因为它是直传接口，协议简单点好
		http.Error(w, "Method not allowed", 405)
		return
	}

	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "missing key", 400)
		return
	}

	// 3. 写入存储
	// SaveRaw 内部是 io.Copy，这个过程可能会很长，
	// 只要 Server 的 ReadTimeout 是 0，这里就不会被断开
	if err := h.pkgMgr.SaveRaw(key, r.Body); err != nil {
		http.Error(w, fmt.Sprintf("save file failed: %v", err), 500)
		return
	}

	// 4. 返回成功
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}
