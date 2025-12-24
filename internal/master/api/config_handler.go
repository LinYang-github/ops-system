package api

import (
	"encoding/json"
	"net/http"
	"net/url"

	"ops-system/internal/master/manager"
)

// handleNacosSettings 获取/保存连接配置
func (h *ServerHandler) NacosSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		cfg, err := h.configMgr.GetNacosConfig() // 注意：这里需要通过 pkgManager 或 server.go 注入
		if err != nil {
			// 没配置过返回空对象，状态 200
			json.NewEncoder(w).Encode(map[string]string{})
			return
		}
		// 处于安全考虑，不返回密码
		cfg.Password = "******"
		json.NewEncoder(w).Encode(cfg)
		return
	}

	if r.Method == http.MethodPost {
		var cfg manager.NacosConfig
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		if err := h.configMgr.SaveNacosConfig(cfg); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Write([]byte(`{"status":"ok"}`))
	}
}

// handleNacosNamespaces 获取命名空间列表
func (h *ServerHandler) NacosNamespaces(w http.ResponseWriter, r *http.Request) {
	// Nacos API: /nacos/v1/console/namespaces
	res, err := h.configMgr.ProxyGet("/nacos/v1/console/namespaces", url.Values{})
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(res)
}

// handleNacosConfigs 获取配置列表
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

	res, err := h.configMgr.ProxyGet("/nacos/v1/cs/configs", params)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(res)
}

// handleNacosConfigDetail 获取具体配置内容
func (h *ServerHandler) NacosConfigDetail(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	params := url.Values{}
	params.Set("dataId", q.Get("dataId"))
	params.Set("group", q.Get("group"))
	if t := q.Get("tenant"); t != "" {
		params.Set("tenant", t)
	}

	// 这里的接口还是列表接口，Nacos 2.x 获取详情通常也是 GET /cs/configs?show=all...
	// 或者直接 GET /cs/configs?dataId=..&group=.. 返回纯文本
	// 这里我们直接代理，前端收到的是纯文本
	res, err := h.configMgr.ProxyGet("/nacos/v1/cs/configs", params)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Write(res)
}

// handleNacosPublish 发布/修改配置
func (h *ServerHandler) NacosPublish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "405", 405)
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
	json.NewDecoder(r.Body).Decode(&req)

	form := url.Values{}
	form.Set("dataId", req.DataId)
	form.Set("group", req.Group)
	form.Set("content", req.Content)
	form.Set("type", req.Type)
	if req.Tenant != "" {
		form.Set("tenant", req.Tenant)
	}

	res, err := h.configMgr.ProxyPost("/nacos/v1/cs/configs", form)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(res)
}

// handleNacosDelete 删除配置
func (h *ServerHandler) NacosDelete(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DataId string `json:"dataId"`
		Group  string `json:"group"`
		Tenant string `json:"tenant"`
	}
	json.NewDecoder(r.Body).Decode(&req)

	if err := h.configMgr.ProxyDelete(req.DataId, req.Group, req.Tenant); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Write([]byte(`{"status":"ok"}`))
}
