package manager

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"ops-system/pkg/config"
)

type ConfigManager struct {
	db         *sql.DB
	mu         sync.RWMutex
	nacosToken string // 内存缓存 Token
	nacosBase  string // 内存缓存 URL
}

func NewConfigManager(db *sql.DB) *ConfigManager {
	return &ConfigManager{db: db}
}

// NacosConfig 对应 sys_settings 中 key="nacos_config" 的结构
type NacosConfig struct {
	URL      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// SaveNacosConfig 保存连接信息
func (cm *ConfigManager) SaveNacosConfig(cfg NacosConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// 存库
	bytes, _ := json.Marshal(cfg)
	_, err := cm.db.Exec(`INSERT OR REPLACE INTO sys_settings (key, value, updated_at) VALUES (?, ?, ?)`,
		"nacos_config", string(bytes), time.Now().Unix())

	// 清理缓存，触发下次重新登录
	cm.nacosToken = ""
	cm.nacosBase = ""

	return err
}

// GetNacosConfig 读取连接信息
func (cm *ConfigManager) GetNacosConfig() (*NacosConfig, error) {
	var val string
	err := cm.db.QueryRow(`SELECT value FROM sys_settings WHERE key = 'nacos_config'`).Scan(&val)
	if err != nil {
		return nil, err
	}
	var cfg NacosConfig
	if err := json.Unmarshal([]byte(val), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// --- Nacos API 封装 ---

// login 内部方法：获取 Token
func (cm *ConfigManager) login() error {
	// 如果内存里有且没过期(简单处理)，直接用
	if cm.nacosToken != "" && cm.nacosBase != "" {
		return nil
	}

	cfg, err := cm.GetNacosConfig()
	if err != nil {
		return fmt.Errorf("nacos config not found, please configure first")
	}

	baseURL := strings.TrimRight(cfg.URL, "/")
	// Nacos Login API
	resp, err := http.PostForm(baseURL+"/nacos/v1/auth/login", url.Values{
		"username": {cfg.Username},
		"password": {cfg.Password},
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("login failed, status: %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var resMap map[string]interface{}
	if err := json.Unmarshal(body, &resMap); err != nil {
		return err
	}

	token, ok := resMap["accessToken"].(string)
	if !ok {
		return fmt.Errorf("invalid response, no accessToken")
	}

	cm.mu.Lock()
	cm.nacosToken = token
	cm.nacosBase = baseURL
	cm.mu.Unlock()
	return nil
}

// ProxyGet 通用 GET 请求代理
func (cm *ConfigManager) ProxyGet(apiPath string, params url.Values) ([]byte, error) {
	if err := cm.login(); err != nil {
		return nil, err
	}

	cm.mu.RLock()
	targetURL := cm.nacosBase + apiPath
	params.Set("accessToken", cm.nacosToken) // 鉴权
	fullURL := targetURL + "?" + params.Encode()
	cm.mu.RUnlock()

	resp, err := http.Get(fullURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 403 {
		// Token 过期，简单重置
		cm.mu.Lock()
		cm.nacosToken = ""
		cm.mu.Unlock()
		return nil, fmt.Errorf("nacos token expired, please retry")
	}

	return io.ReadAll(resp.Body)
}

// ProxyPost 通用 POST 请求代理
func (cm *ConfigManager) ProxyPost(apiPath string, data url.Values) ([]byte, error) {
	if err := cm.login(); err != nil {
		return nil, err
	}

	cm.mu.RLock()
	targetURL := cm.nacosBase + apiPath
	data.Set("accessToken", cm.nacosToken)
	cm.mu.RUnlock()

	resp, err := http.PostForm(targetURL, data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}

// ProxyDelete 删除配置
func (cm *ConfigManager) ProxyDelete(dataId, group, tenant string) error {
	if err := cm.login(); err != nil {
		return err
	}

	cm.mu.RLock()
	targetURL := cm.nacosBase + "/nacos/v1/cs/configs"
	cm.mu.RUnlock()

	client := &http.Client{}
	req, _ := http.NewRequest("DELETE", targetURL, nil)

	q := req.URL.Query()
	q.Add("accessToken", cm.nacosToken)
	q.Add("dataId", dataId)
	q.Add("group", group)
	if tenant != "" {
		q.Add("tenant", tenant)
	}
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("delete failed: %s", string(body))
	}
	return nil
}

// GetGlobalConfig 获取全局配置 (如果 DB 没有，返回默认值)
func (cm *ConfigManager) GetGlobalConfig() (*config.GlobalConfig, error) {
	var val string
	err := cm.db.QueryRow(`SELECT value FROM sys_settings WHERE key = 'global_config'`).Scan(&val)

	cfg := config.DefaultGlobalConfig() // 默认值

	if err == sql.ErrNoRows {
		return &cfg, nil // 返回默认
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(val), &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveGlobalConfig 保存全局配置
func (cm *ConfigManager) SaveGlobalConfig(cfg config.GlobalConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	bytes, _ := json.Marshal(cfg)
	_, err := cm.db.Exec(`INSERT OR REPLACE INTO sys_settings (key, value, updated_at) VALUES (?, ?, ?)`,
		"global_config", string(bytes), time.Now().Unix())

	return err
}
