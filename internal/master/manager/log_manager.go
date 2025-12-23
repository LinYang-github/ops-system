package manager

import (
	"database/sql"
	"ops-system/pkg/protocol"
	"time"
)

type LogManager struct {
	db *sql.DB
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
