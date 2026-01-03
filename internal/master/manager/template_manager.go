package manager

import (
	"bytes"
	"database/sql"
	"fmt"
	"text/template"
	"time"

	"ops-system/pkg/protocol"

	"github.com/google/uuid"
)

type TemplateManager struct {
	db *sql.DB
}

func NewTemplateManager(db *sql.DB) *TemplateManager {
	return &TemplateManager{db: db}
}

// CreateTemplate 创建模板
func (tm *TemplateManager) CreateTemplate(name, content, format string) error {
	id := uuid.NewString()
	_, err := tm.db.Exec(`INSERT INTO config_templates (id, name, content, format, update_time) VALUES (?, ?, ?, ?, ?)`,
		id, name, content, format, time.Now().Unix())
	return err
}

// UpdateTemplate 更新模板
func (tm *TemplateManager) UpdateTemplate(id, name, content, format string) error {
	_, err := tm.db.Exec(`UPDATE config_templates SET name=?, content=?, format=?, update_time=? WHERE id=?`,
		name, content, format, time.Now().Unix(), id)
	return err
}

// DeleteTemplate 删除模板
func (tm *TemplateManager) DeleteTemplate(id string) error {
	_, err := tm.db.Exec(`DELETE FROM config_templates WHERE id=?`, id)
	return err
}

// ListTemplates 获取列表
func (tm *TemplateManager) ListTemplates() ([]protocol.ConfigTemplate, error) {
	rows, err := tm.db.Query(`SELECT id, name, content, format, update_time FROM config_templates ORDER BY update_time DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []protocol.ConfigTemplate
	for rows.Next() {
		var t protocol.ConfigTemplate
		rows.Scan(&t.ID, &t.Name, &t.Content, &t.Format, &t.UpdateTime)
		list = append(list, t)
	}
	return list, nil
}

// GetTemplate 获取单个详情
func (tm *TemplateManager) GetTemplate(id string) (*protocol.ConfigTemplate, error) {
	var t protocol.ConfigTemplate
	err := tm.db.QueryRow(`SELECT id, name, content, format, update_time FROM config_templates WHERE id=?`, id).
		Scan(&t.ID, &t.Name, &t.Content, &t.Format, &t.UpdateTime)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// Render 核心渲染方法
// ctx: 渲染上下文，包含 Node, Service 等信息
func (tm *TemplateManager) Render(templateID string, ctx interface{}) (string, error) {
	tpl, err := tm.GetTemplate(templateID)
	if err != nil {
		return "", fmt.Errorf("template not found: %s", templateID)
	}

	// 解析模板
	t, err := template.New(tpl.Name).Parse(tpl.Content)
	if err != nil {
		return "", fmt.Errorf("parse template failed: %v", err)
	}

	// 执行渲染
	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("execute template failed: %v", err)
	}

	return buf.String(), nil
}
