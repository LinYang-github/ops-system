package task

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"ops-system/pkg/utils" // 复用 HTTP Client

	_ "modernc.org/sqlite"
)

type Task struct {
	ID         string
	Type       string
	InstanceID string
	NodeIP     string
	Payload    string // JSON
	Status     string
	RetryCount int
	MaxRetries int
	NextRetry  int64
	ErrorMsg   string
}

type TaskManager struct {
	db *sql.DB
	mu sync.Mutex
}

func NewTaskManager(db *sql.DB) *TaskManager {
	return &TaskManager{db: db}
}

// AddTask 添加任务 (API 调用)
func (tm *TaskManager) AddTask(taskType, instID, nodeIP string, payload interface{}) error {
	payloadBytes, _ := json.Marshal(payload)
	id := fmt.Sprintf("task-%d", time.Now().UnixNano())

	query := `
		INSERT INTO sys_tasks 
		(id, type, instance_id, node_ip, payload, status, retry_count, max_retries, next_retry, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, 'pending', 0, 5, ?, ?, ?)
	`
	now := time.Now().Unix()
	_, err := tm.db.Exec(query, id, taskType, instID, nodeIP, string(payloadBytes), now, now, now)
	return err
}

// StartDispatcher 启动调度器 (在 main.go 中启动)
func (tm *TaskManager) StartDispatcher() {
	go func() {
		for {
			tm.processPendingTasks()
			time.Sleep(1 * time.Second) // 轮询间隔
		}
	}()
}

func (tm *TaskManager) processPendingTasks() {
	now := time.Now().Unix()
	// 查询待执行任务
	rows, err := tm.db.Query(`
		SELECT id, type, instance_id, node_ip, payload, retry_count 
		FROM sys_tasks 
		WHERE status IN ('pending', 'retrying') AND next_retry <= ? 
		LIMIT 50`, now) // 每次取 50 个，起到流量控制作用

	if err != nil {
		log.Printf("[Task] Poll error: %v", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var t Task
		rows.Scan(&t.ID, &t.Type, &t.InstanceID, &t.NodeIP, &t.Payload, &t.RetryCount)

		// 异步执行，不阻塞轮询
		go tm.executeTask(t)
	}
}

func (tm *TaskManager) executeTask(t Task) {
	// 1. 标记为处理中
	tm.db.Exec("UPDATE sys_tasks SET status='processing' WHERE id=?", t.ID)

	// 2. 获取 Worker 地址
	node, exists := store.GetNode(t.NodeIP)
	var err error

	if !exists {
		err = fmt.Errorf("node offline")
	} else {
		// 3. 发送请求
		var targetURL string
		if t.Type == "deploy" {
			targetURL = fmt.Sprintf("%s/api/deploy", node.WorkerURL)
		} else {
			targetURL = fmt.Sprintf("%s/api/instance/action", node.WorkerURL)
		}

		// 使用之前的 utils.PostJSON
		err = utils.PostJSON(targetURL, []byte(t.Payload))
	}

	// 4. 处理结果
	now := time.Now().Unix()
	if err == nil {
		// 成功
		tm.db.Exec("UPDATE sys_tasks SET status='success', updated_at=? WHERE id=?", now, t.ID)
		log.Printf("[Task] %s success", t.ID)
	} else {
		// 失败 -> 重试逻辑
		log.Printf("[Task] %s failed: %v. Retrying...", t.ID, err)

		if t.RetryCount >= 5 { // 这里的 5 应与 max_retries 一致
			tm.db.Exec("UPDATE sys_tasks SET status='failed', error_msg=?, updated_at=? WHERE id=?", err.Error(), now, t.ID)
		} else {
			// 指数退避: 2^retry_count 秒 (1, 2, 4, 8, 16)
			backoff := int64(1 << t.RetryCount)
			next := now + backoff
			tm.db.Exec(`
				UPDATE sys_tasks 
				SET status='retrying', retry_count=retry_count+1, next_retry=?, error_msg=?, updated_at=? 
				WHERE id=?`, next, err.Error(), now, t.ID)
		}
	}
}
