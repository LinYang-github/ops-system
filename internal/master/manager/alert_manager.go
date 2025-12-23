package manager

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"

	"ops-system/internal/master/ws" // 用于推送
	"ops-system/pkg/protocol"
)

// stateKey 用于内存中追踪告警状态
type stateKey struct {
	RuleID   int64
	TargetID string
}

type alertState struct {
	FirstTriggerTime int64 // 第一次满足条件的时间 (用于防抖)
	IsFiring         bool  // 是否已经触发告警
	EventID          int64 // 数据库中的 Event ID (用于更新 EndTime)
}

type AlertManager struct {
	db      *sql.DB
	nodeMgr *NodeManager
	instMgr *InstanceManager

	mu     sync.RWMutex
	states map[stateKey]*alertState // 内存状态机
}

func NewAlertManager(db *sql.DB, nm *NodeManager, im *InstanceManager) *AlertManager {
	am := &AlertManager{
		db:      db,
		nodeMgr: nm,
		instMgr: im,
		states:  make(map[stateKey]*alertState),
	}
	go am.runEvaluationLoop()
	return am
}

// --- 规则管理 ---

func (am *AlertManager) AddRule(r protocol.AlertRule) error {
	am.mu.Lock()
	defer am.mu.Unlock()
	_, err := am.db.Exec(`INSERT INTO sys_alert_rules (name, target_type, metric, condition, threshold, duration, enabled) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		r.Name, r.TargetType, r.Metric, r.Condition, r.Threshold, r.Duration, true)
	return err
}

func (am *AlertManager) DeleteRule(id int64) error {
	am.mu.Lock()
	defer am.mu.Unlock()
	_, err := am.db.Exec("DELETE FROM sys_alert_rules WHERE id = ?", id)
	return err
}

func (am *AlertManager) GetRules() ([]*protocol.AlertRule, error) {
	rows, err := am.db.Query("SELECT id, name, target_type, metric, condition, threshold, duration, enabled FROM sys_alert_rules")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*protocol.AlertRule
	for rows.Next() {
		var r protocol.AlertRule
		rows.Scan(&r.ID, &r.Name, &r.TargetType, &r.Metric, &r.Condition, &r.Threshold, &r.Duration, &r.Enabled)
		list = append(list, &r)
	}
	return list, nil
}

func (am *AlertManager) GetActiveEvents() ([]*protocol.AlertEvent, error) {
	// 查询未结束的告警 (status = 'firing')
	rows, err := am.db.Query("SELECT id, rule_name, target_type, target_id, target_name, metric_val, message, start_time FROM sys_alert_events WHERE status = 'firing' ORDER BY start_time DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*protocol.AlertEvent
	for rows.Next() {
		var e protocol.AlertEvent
		rows.Scan(&e.ID, &e.RuleName, &e.TargetType, &e.TargetID, &e.TargetName, &e.MetricVal, &e.Message, &e.StartTime)
		e.Status = "firing"
		list = append(list, &e)
	}
	return list, nil
}

func (am *AlertManager) GetHistoryEvents(limit int) ([]*protocol.AlertEvent, error) {
	rows, err := am.db.Query("SELECT id, rule_name, target_type, target_name, message, status, start_time, end_time FROM sys_alert_events ORDER BY start_time DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*protocol.AlertEvent
	for rows.Next() {
		var e protocol.AlertEvent
		rows.Scan(&e.ID, &e.RuleName, &e.TargetType, &e.TargetName, &e.Message, &e.Status, &e.StartTime, &e.EndTime)
		list = append(list, &e)
	}
	return list, nil
}

// --- 评估引擎 (核心) ---

func (am *AlertManager) runEvaluationLoop() {
	ticker := time.NewTicker(5 * time.Second) // 5秒检查一次
	for range ticker.C {
		am.evaluate()
	}
}

func (am *AlertManager) evaluate() {
	rules, _ := am.GetRules()
	if len(rules) == 0 {
		return
	}

	// 获取快照
	nodes := am.nodeMgr.GetAllNodesMetrics()
	instances := am.instMgr.GetAllInstancesMetrics()

	am.mu.Lock()
	defer am.mu.Unlock()

	now := time.Now().Unix()

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		// 根据规则类型遍历目标
		if rule.TargetType == "node" {
			for _, node := range nodes {
				val, triggered := checkCondition(rule, node.Status, node.CPUUsage, node.MemUsage)
				am.handleState(rule, node.IP, node.Hostname, val, triggered, now)
			}
		} else if rule.TargetType == "instance" {
			for _, inst := range instances {
				val, triggered := checkCondition(rule, inst.Status, inst.CpuUsage, float64(inst.MemUsage))
				targetName := fmt.Sprintf("%s (%s)", inst.ServiceName, inst.NodeIP)
				am.handleState(rule, inst.ID, targetName, val, triggered, now)
			}
		}
	}
}

// 辅助：检查数值是否满足条件
func checkCondition(rule *protocol.AlertRule, status string, cpu, mem float64) (float64, bool) {
	var currentVal float64

	// 特殊处理状态检查
	if rule.Metric == "status" {
		// 约定：如果 rule.Condition 是 "="，且 rule.Threshold 是 0(offline/stopped) 或 1(online/running)
		// 这里简化：如果 metric 是 status，我们认为 offline/stopped/error 是异常(1), online/running 是正常(0)
		isAbnormal := false
		if status != "online" && status != "running" {
			isAbnormal = true
		}
		// 如果规则是 "> 0"，则异常时触发
		if isAbnormal {
			return 1, true
		}
		return 0, false
	}

	if rule.Metric == "cpu" {
		currentVal = cpu
	}
	if rule.Metric == "mem" {
		currentVal = mem
	}

	if rule.Condition == ">" && currentVal > rule.Threshold {
		return currentVal, true
	}
	if rule.Condition == "<" && currentVal < rule.Threshold {
		return currentVal, true
	}
	return currentVal, false
}

// 辅助：状态流转 (Pending -> Firing -> Resolved)
func (am *AlertManager) handleState(rule *protocol.AlertRule, targetID, targetName string, val float64, triggered bool, now int64) {
	key := stateKey{RuleID: rule.ID, TargetID: targetID}
	state, exists := am.states[key]

	if triggered {
		if !exists {
			// 1. 首次触发 -> 进入 Pending
			am.states[key] = &alertState{FirstTriggerTime: now, IsFiring: false}
		} else {
			// 2. 持续触发
			if !state.IsFiring {
				// 检查是否达到 Duration
				if now-state.FirstTriggerTime >= int64(rule.Duration) {
					// -> Firing (记录数据库 + 广播)
					eventID := am.fireAlert(rule, targetID, targetName, val)
					state.IsFiring = true
					state.EventID = eventID
				}
			}
			// 如果已经是 Firing，保持现状 (暂不重复发通知)
		}
	} else {
		if exists {
			// 3. 恢复正常
			if state.IsFiring {
				// -> Resolved
				am.resolveAlert(state.EventID)
			}
			// 清除状态
			delete(am.states, key)
		}
	}
}

func (am *AlertManager) fireAlert(rule *protocol.AlertRule, targetID, targetName string, val float64) int64 {
	msg := fmt.Sprintf("[%s] %s %s %.1f (Threshold: %.1f)", rule.Name, targetName, rule.Metric, val, rule.Threshold)
	log.Printf("🔥 ALERT FIRING: %s", msg)

	// 写入 DB
	res, _ := am.db.Exec(`INSERT INTO sys_alert_events (rule_id, rule_name, target_type, target_id, target_name, metric_val, message, status, start_time) VALUES (?, ?, ?, ?, ?, ?, ?, 'firing', ?)`,
		rule.ID, rule.Name, rule.TargetType, targetID, targetName, val, msg, time.Now().Unix())

	id, _ := res.LastInsertId()

	// WebSocket 广播
	ws.BroadcastAlerts(map[string]interface{}{
		"type": "fire", "message": msg, "target": targetName,
	})

	return id
}

func (am *AlertManager) resolveAlert(eventID int64) {
	log.Printf("✅ ALERT RESOLVED: Event %d", eventID)
	am.db.Exec(`UPDATE sys_alert_events SET status = 'resolved', end_time = ? WHERE id = ?`, time.Now().Unix(), eventID)

	// 广播
	ws.BroadcastAlerts(map[string]interface{}{
		"type": "resolve", "id": eventID,
	})
}

// DeleteEvent 删除单个告警记录
func (am *AlertManager) DeleteEvent(id int64) error {
	am.mu.Lock()
	defer am.mu.Unlock()
	_, err := am.db.Exec("DELETE FROM sys_alert_events WHERE id = ?", id)
	return err
}

// ClearEvents 清空所有告警记录
func (am *AlertManager) ClearEvents() error {
	am.mu.Lock()
	defer am.mu.Unlock()
	// 使用 TRUNCATE 或者 DELETE
	// SQLite 使用 DELETE FROM table; 即使有自增主键也不影响
	_, err := am.db.Exec("DELETE FROM sys_alert_events")
	return err
}
