package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"ops-system/internal/master/manager"
	"ops-system/pkg/code"
	"ops-system/pkg/config"
	"ops-system/pkg/e"
	"ops-system/pkg/response"
	"ops-system/pkg/utils"
)

// NacosSettings 获取/保存连接配置
// GET/POST /api/nacos/settings
func (h *ServerHandler) NacosSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		cfg, err := h.configMgr.GetNacosConfig()
		if err != nil {
			// 没配置过返回空对象，不算错误
			response.Success(w, map[string]string{})
			return
		}
		// 处于安全考虑，不返回密码
		cfg.Password = "******"
		response.Success(w, cfg)
		return
	}

	if r.Method == http.MethodPost {
		var cfg manager.NacosConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
			return
		}
		if err := h.configMgr.SaveNacosConfig(cfg); err != nil {
			response.Error(w, e.New(code.DatabaseError, "保存配置失败", err))
			return
		}
		response.Success(w, nil)
		return
	}

	response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
}

// NacosNamespaces 获取命名空间列表
// GET /api/nacos/namespaces
func (h *ServerHandler) NacosNamespaces(w http.ResponseWriter, r *http.Request) {
	// Nacos API: /nacos/v1/console/namespaces
	resBytes, err := h.configMgr.ProxyGet("/nacos/v1/console/namespaces", url.Values{})
	if err != nil {
		response.Error(w, e.New(code.NacosError, "获取命名空间失败", err))
		return
	}

	// 将 Nacos 返回的 JSON 字节流转为对象，以便包裹在标准响应结构中
	var nacosResp interface{}
	if err := json.Unmarshal(resBytes, &nacosResp); err != nil {
		// 如果转换失败（可能是纯文本报错），直接返回字符串
		response.Success(w, string(resBytes))
		return
	}

	response.Success(w, nacosResp)
}

// NacosConfigs 获取配置列表
// GET /api/nacos/configs
func (h *ServerHandler) NacosConfigs(w http.ResponseWriter, r *http.Request) {
	// Nacos API: /nacos/v1/cs/configs
	q := r.URL.Query()
	params := url.Values{}
	params.Set("dataId", q.Get("dataId"))
	params.Set("group", q.Get("group"))
	params.Set("pageNo", q.Get("pageNo"))
	params.Set("pageSize", q.Get("pageSize"))
	if t := q.Get("tenant"); t != "" {
		params.Set("tenant", t) // Namespace ID
	}

	resBytes, err := h.configMgr.ProxyGet("/nacos/v1/cs/configs", params)
	if err != nil {
		response.Error(w, e.New(code.NacosError, "查询配置列表失败", err))
		return
	}

	var nacosResp interface{}
	if err := json.Unmarshal(resBytes, &nacosResp); err != nil {
		response.Success(w, string(resBytes))
		return
	}
	response.Success(w, nacosResp)
}

// NacosConfigDetail 获取具体配置内容
// GET /api/nacos/config/detail
func (h *ServerHandler) NacosConfigDetail(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params := url.Values{}
	params.Set("dataId", q.Get("dataId"))
	params.Set("group", q.Get("group"))
	if t := q.Get("tenant"); t != "" {
		params.Set("tenant", t)
	}

	// Nacos 获取详情直接返回配置内容字符串
	resBytes, err := h.configMgr.ProxyGet("/nacos/v1/cs/configs", params)
	if err != nil {
		response.Error(w, e.New(code.NacosError, "获取配置详情失败", err))
		return
	}

	// 直接返回内容字符串，response.Success 会将其放入 "data" 字段
	response.Success(w, string(resBytes))
}

// NacosPublish 发布/修改配置
// POST /api/nacos/config/publish
func (h *ServerHandler) NacosPublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	// 前端传 JSON，我们需要转成 Form Data 发给 Nacos
	var req struct {
		DataId  string `json:"dataId"`
		Group   string `json:"group"`
		Content string `json:"content"`
		Type    string `json:"type"`
		Tenant  string `json:"tenant"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	form := url.Values{}
	form.Set("dataId", req.DataId)
	form.Set("group", req.Group)
	form.Set("content", req.Content)
	form.Set("type", req.Type)
	if req.Tenant != "" {
		form.Set("tenant", req.Tenant)
	}

	resBytes, err := h.configMgr.ProxyPost("/nacos/v1/cs/configs", form)
	if err != nil {
		response.Error(w, e.New(code.NacosError, fmt.Sprintf("发布配置失败: %v", err), err))
		return
	}

	// Nacos 发布成功通常返回 "true" 字符串
	response.Success(w, string(resBytes))
}

// NacosDelete 删除配置
// POST /api/nacos/config/delete
func (h *ServerHandler) NacosDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, e.New(code.MethodNotAllowed, "Method not allowed", nil))
		return
	}

	var req struct {
		DataId string `json:"dataId"`
		Group  string `json:"group"`
		Tenant string `json:"tenant"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "JSON解析失败", err))
		return
	}

	if err := h.configMgr.ProxyDelete(req.DataId, req.Group, req.Tenant); err != nil {
		response.Error(w, e.New(code.NacosError, fmt.Sprintf("删除配置失败: %v", err), err))
		return
	}

	response.Success(w, nil)
}

// handleGetGlobalConfig 获取系统设置
func (h *ServerHandler) GetGlobalConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.configMgr.GetGlobalConfig()
	if err != nil {
		response.Error(w, e.New(code.DatabaseError, "读取配置失败", err))
		return
	}
	response.Success(w, cfg)
}

// handleUpdateGlobalConfig 更新系统设置 (含热更新逻辑)
func (h *ServerHandler) UpdateGlobalConfig(w http.ResponseWriter, r *http.Request) {
	var cfg config.GlobalConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		response.Error(w, e.New(code.InvalidJSON, "配置格式错误", err))
		return
	}

	// 1. 保存到数据库
	if err := h.configMgr.SaveGlobalConfig(cfg); err != nil {
		response.Error(w, e.New(code.DatabaseError, "保存配置失败", err))
		return
	}

	// 2. 触发 Master 内存热更新
	// 更新 HTTP Client
	utils.SetTimeout(time.Duration(cfg.Logic.HTTPClientTimeout) * time.Second)
	// 更新 Node Manager
	h.nodeMgr.SetOfflineThreshold(time.Duration(cfg.Logic.NodeOfflineThreshold) * time.Second)

	// 3.异步触发日志清理
	go func() {
		h.logMgr.CleanupOldLogs(cfg.Log.RetentionDays)
	}()

	// 4. (可选) 记录审计日志
	h.logMgr.RecordLog(utils.GetClientIP(r), "update_config", "system", "global", "Updated system settings", "success")

	response.Success(w, nil)
}
