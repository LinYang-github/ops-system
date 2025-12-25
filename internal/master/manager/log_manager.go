package manager

import (
	"database/sql"
	"log"
	"ops-system/pkg/protocol"
	"sync"
	"time"
)

type LogManager struct {
	db *sql.DB
	mu sync.Mutex
}

func NewLogManager(db *sql.DB) *LogManager {
	return &LogManager{db: db}
}

func (lm *LogManager) RecordLog(operator, action, targetType, targetName, detail, status string) {
	go func() {
		query := `INSERT INTO sys_op_logs (operator, action, target_type, target_name, detail, status, create_time) VALUES (?, ?, ?, ?, ?, ?, ?)`
		lm.db.Exec(query, operator, action, targetType, targetName, detail, status, time.Now().Unix())
	}()
}

func (lm *LogManager) GetLogs(page, pageSize int, keyword string) (*protocol.LogQueryResp, error) {
	offset := (page - 1) * pageSize
	resp := &protocol.LogQueryResp{List: []*protocol.OpLog{}}

	// 1. 查总数
	countQuery := `SELECT COUNT(*) FROM sys_op_logs`
	var args []interface{}

	if keyword != "" {
		countQuery += ` WHERE action LIKE ? OR target_name LIKE ? OR operator LIKE ?`
		pattern := "%" + keyword + "%"
		args = append(args, pattern, pattern, pattern)
	}

	err := lm.db.QueryRow(countQuery, args...).Scan(&resp.Total)
	if err != nil {
		return nil, err
	}

	// 2. 查列表
	listQuery := `SELECT id, operator, action, target_type, target_name, detail, status, create_time 
				  FROM sys_op_logs`
	if keyword != "" {
		listQuery += ` WHERE action LIKE ? OR target_name LIKE ? OR operator LIKE ?`
	}
	listQuery += ` ORDER BY create_time DESC LIMIT ? OFFSET ?`
	args = append(args, pageSize, offset)

	rows, err := lm.db.Query(listQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var l protocol.OpLog
		rows.Scan(&l.ID, &l.Operator, &l.Action, &l.TargetType, &l.TargetName, &l.Detail, &l.Status, &l.CreateTime)
		resp.List = append(resp.List, &l)
	}

	return resp, nil
}

// CleanupOldLogs 清理 N 天前的日志
func (lm *LogManager) CleanupOldLogs(days int) error {
	if days <= 0 {
		return nil // 不限制
	}

	// 计算截止时间戳
	// time.AddDate(0, 0, -days) 表示当前时间减去 days 天
	cutoff := time.Now().AddDate(0, 0, -days).Unix()

	lm.mu.Lock() // 假设你给 LogManager 加了锁，如果没有加锁，db.Exec 本身是并发安全的，可视情况加锁
	defer lm.mu.Unlock()

	res, err := lm.db.Exec("DELETE FROM sys_op_logs WHERE create_time < ?", cutoff)
	if err != nil {
		log.Printf("[LogManager] Cleanup failed: %v", err)
		return err
	}

	rows, _ := res.RowsAffected()
	if rows > 0 {
		log.Printf("[LogManager] Cleaned up %d log entries older than %d days", rows, days)
	}
	return nil
}
