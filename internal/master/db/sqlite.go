package db

import (
	"database/sql"
	"log"

	_ "modernc.org/sqlite"
)

// InitDB 初始化并返回数据库连接
// 【修复点】增加 dbPath 参数，匹配 server.go 的调用
func InitDB(dbPath string) *sql.DB {
	log.Printf(">>> DB PATH: %s", dbPath)

	// 使用传入的 dbPath，而不是自己在内部计算
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		log.Fatalf("Failed to open db: %v", err)
	}

	initTables(db)

	return db
}

func initTables(db *sql.DB) {
	sqls := []string{
		// 系统表
		`CREATE TABLE IF NOT EXISTS system_infos (id TEXT PRIMARY KEY, name TEXT, description TEXT, create_time INTEGER);`,
		// 模块表
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
			readiness_timeout INTEGER
		);`,
		// 实例表
		`CREATE TABLE IF NOT EXISTS instance_infos (id TEXT PRIMARY KEY, system_id TEXT, node_ip TEXT, service_name TEXT, service_version TEXT, status TEXT, pid INTEGER, uptime INTEGER);`,
		// 日志表
		`CREATE TABLE IF NOT EXISTS sys_op_logs (id INTEGER PRIMARY KEY AUTOINCREMENT, operator TEXT, action TEXT, target_type TEXT, target_name TEXT, detail TEXT, status TEXT, create_time INTEGER);`,

		// 节点表
		`CREATE TABLE IF NOT EXISTS node_infos (
			id TEXT PRIMARY KEY, 
			ip TEXT,
			port INTEGER,
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

		// 服务包元数据表
		// 联合主键：name + version
		`CREATE TABLE IF NOT EXISTS package_infos (
			name TEXT,
			version TEXT,
			size INTEGER,
			upload_time INTEGER,
			manifest TEXT, -- 存储 service.json 的完整内容
			PRIMARY KEY (name, version)
		);`,

		// 通用配置表
		`CREATE TABLE IF NOT EXISTS sys_settings (
			key TEXT PRIMARY KEY,
			value TEXT,
			updated_at INTEGER
		);`,

		// 告警规则表
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

		// 告警事件表 (记录历史)
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
	}

	for _, sqlStmt := range sqls {
		if _, err := db.Exec(sqlStmt); err != nil {
			log.Fatalf("Failed to init table: %v\nSQL: %s", err, sqlStmt)
		}
	}
}

// CloseDB 关闭数据库连接 (用于恢复备份前释放锁)
func CloseDB(db *sql.DB) error {
	log.Println(">>> Closing Database Connection...")
	return db.Close()
}
