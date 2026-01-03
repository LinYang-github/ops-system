package db

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

// InitDB 初始化并返回数据库连接
// 【修复点】增加 dbPath 参数，匹配 server.go 的调用
func InitDB(dbPath string) *sql.DB {
	log.Printf(">>> DB PATH: %s", dbPath)

	// 1. 确保 DSN 包含 busy_timeout 和 WAL 模式
	// 增加 cache=shared 可以提升并发性能
	dsn := fmt.Sprintf("%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("Failed to open db: %v", err)
	}

	// 2. 【关键修正】放开连接限制
	// 建议设置为一个合理的数值（如 10），或者 0（不限制）
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(time.Hour)

	initTables(db)

	return db
}

func initTables(db *sql.DB) {
	// 所有表结构均采用当前系统的最终版本字段定义
	sqls := []string{
		// 业务系统定义
		`CREATE TABLE IF NOT EXISTS system_infos (
			id TEXT PRIMARY KEY, 
			name TEXT, 
			description TEXT, 
			create_time INTEGER
		);`,

		// 业务系统模块 (已包含所有最新编排字段)
		`CREATE TABLE IF NOT EXISTS system_modules (
			id TEXT PRIMARY KEY, 
			system_id TEXT, 
			module_name TEXT, 
			package_name TEXT, 
			package_version TEXT, 
			description TEXT,
			start_order INTEGER,
			readiness_type TEXT, 
			readiness_target TEXT,
			readiness_timeout INTEGER,
			config_mounts TEXT DEFAULT '[]',
			env_vars TEXT DEFAULT '{}'
		);`,

		// 实例运行状态
		`CREATE TABLE IF NOT EXISTS instance_infos (
			id TEXT PRIMARY KEY, 
			system_id TEXT, 
			node_id TEXT, 
			service_name TEXT, 
			service_version TEXT, 
			status TEXT, 
			pid INTEGER, 
			uptime INTEGER
		);`,

		// 操作审计日志
		`CREATE TABLE IF NOT EXISTS sys_op_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT, 
			operator TEXT, 
			action TEXT, 
			target_type TEXT, 
			target_name TEXT, 
			detail TEXT, 
			status TEXT, 
			create_time INTEGER
		);`,

		// 节点基础信息
		`CREATE TABLE IF NOT EXISTS node_infos (
			id TEXT PRIMARY KEY, 
			ip TEXT,
			hostname TEXT,
			name TEXT,
			mac_addr TEXT,
			os TEXT,
			arch TEXT,
			cpu_cores INTEGER,
			mem_total INTEGER,
			disk_total INTEGER,
			status TEXT,
			last_heartbeat INTEGER
		);`,

		// 服务包元数据
		`CREATE TABLE IF NOT EXISTS package_infos (
			name TEXT,
			version TEXT,
			size INTEGER,
			upload_time INTEGER,
			manifest TEXT, -- 存储 service.json 的完整 JSON 内容
			PRIMARY KEY (name, version)
		);`,

		// 全局系统设置
		`CREATE TABLE IF NOT EXISTS sys_settings (
			key TEXT PRIMARY KEY,
			value TEXT,
			updated_at INTEGER
		);`,

		// 告警规则
		`CREATE TABLE IF NOT EXISTS sys_alert_rules (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT,
			target_type TEXT,
			metric TEXT,
			condition TEXT,
			threshold REAL,
			duration INTEGER,
			enabled BOOLEAN
		);`,

		// 告警事件历史
		`CREATE TABLE IF NOT EXISTS sys_alert_events (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			rule_id INTEGER,
			rule_name TEXT,
			target_type TEXT,
			target_id TEXT,
			target_name TEXT,
			metric_val REAL,
			message TEXT,
			status TEXT,
			start_time INTEGER,
			end_time INTEGER
		);`,

		// 配置模板 (用于配置文件注入)
		`CREATE TABLE IF NOT EXISTS config_templates (
			id TEXT PRIMARY KEY,
			name TEXT,
			content TEXT,
			format TEXT,
			update_time INTEGER
		);`,
	}

	for _, sqlStmt := range sqls {
		if _, err := db.Exec(sqlStmt); err != nil {
			log.Fatalf("Failed to init table: %v\nSQL: %s", err, sqlStmt)
		}
	}

	log.Println(">>> Database tables initialized successfully.")
}

// CloseDB 关闭数据库连接 (用于恢复备份前释放锁)
func CloseDB(db *sql.DB) error {
	log.Println(">>> Closing Database Connection...")
	return db.Close()
}
