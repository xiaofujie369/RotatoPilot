package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ DB *sql.DB }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.DB.Exec(schema)
	return err
}

func Now() string { return time.Now().UTC().Format(time.RFC3339) }
func Bool(v bool) int {
	if v {
		return 1
	}
	return 0
}
func (s *Store) Log(level, machineID, job, message string, meta any) {
	metaJSON := "{}"
	if meta != nil {
		metaJSON = fmt.Sprint(meta)
	}
	_, _ = s.DB.Exec(`INSERT INTO logs(level,machine_id,job_type,message,meta_json,created_at) VALUES(?,?,?,?,?,?)`, level, machineID, job, message, metaJSON, Now())
}

const schema = `
CREATE TABLE IF NOT EXISTS provider_panels (id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,api_base_url TEXT NOT NULL,token_encrypted TEXT NOT NULL,token_type TEXT DEFAULT 'raw_authorization',enabled INTEGER DEFAULT 1,last_test_status TEXT,last_test_error TEXT,last_test_at TEXT,created_at TEXT,updated_at TEXT);
CREATE TABLE IF NOT EXISTS machines (id TEXT PRIMARY KEY,provider_id INTEGER NOT NULL,name TEXT,remark TEXT,region TEXT,status TEXT,public_ipv4 TEXT,public_ipv6 TEXT,expected_port INTEGER DEFAULT 443,auto_rotate_enabled INTEGER DEFAULT 0,health_status TEXT DEFAULT 'unknown',failure_count INTEGER DEFAULT 0,success_count INTEGER DEFAULT 0,last_check_at TEXT,last_rotation_at TEXT,rotation_cooldown_until TEXT,traffic_text TEXT,expire_time TEXT,raw_provider_json TEXT,created_at TEXT,updated_at TEXT,FOREIGN KEY(provider_id) REFERENCES provider_panels(id));
CREATE TABLE IF NOT EXISTS agents (id TEXT PRIMARY KEY,machine_id TEXT NOT NULL,token_hash TEXT NOT NULL,token_prefix TEXT NOT NULL,name TEXT,hostname TEXT,agent_version TEXT,os TEXT,arch TEXT,public_ipv4 TEXT,public_ipv6 TEXT,status TEXT DEFAULT 'offline',last_heartbeat_at TEXT,revoked INTEGER DEFAULT 0,created_at TEXT,updated_at TEXT,FOREIGN KEY(machine_id) REFERENCES machines(id) ON DELETE CASCADE);
CREATE INDEX IF NOT EXISTS idx_agents_token ON agents(token_hash);
CREATE TABLE IF NOT EXISTS agent_tasks (id TEXT PRIMARY KEY,agent_id TEXT NOT NULL,machine_id TEXT NOT NULL,type TEXT NOT NULL,payload_json TEXT,status TEXT DEFAULT 'pending',result_json TEXT,error TEXT,created_at TEXT,started_at TEXT,finished_at TEXT);
CREATE TABLE IF NOT EXISTS probe_configs (id INTEGER PRIMARY KEY AUTOINCREMENT,machine_id TEXT NOT NULL,name TEXT NOT NULL,source TEXT NOT NULL,type TEXT NOT NULL,target_host TEXT,target_port INTEGER,url TEXT,expected_status TEXT,timeout_ms INTEGER DEFAULT 5000,interval_seconds INTEGER DEFAULT 300,failure_weight INTEGER DEFAULT 1,enabled INTEGER DEFAULT 1,created_at TEXT,updated_at TEXT);
CREATE TABLE IF NOT EXISTS probe_results (id INTEGER PRIMARY KEY AUTOINCREMENT,machine_id TEXT NOT NULL,probe_id INTEGER,source TEXT,probe_type TEXT,target TEXT,success INTEGER,latency_ms INTEGER,error TEXT,checked_at TEXT);
CREATE TABLE IF NOT EXISTS dns_providers (id INTEGER PRIMARY KEY AUTOINCREMENT,name TEXT NOT NULL,provider_type TEXT NOT NULL,token_encrypted TEXT,extra_config_json TEXT,enabled INTEGER DEFAULT 1,created_at TEXT,updated_at TEXT);
CREATE TABLE IF NOT EXISTS dns_records (id INTEGER PRIMARY KEY AUTOINCREMENT,machine_id TEXT NOT NULL,dns_provider_id INTEGER NOT NULL,record_name TEXT NOT NULL,record_type TEXT DEFAULT 'A',zone_id TEXT,proxied INTEGER DEFAULT 0,ttl INTEGER DEFAULT 120,enabled INTEGER DEFAULT 0,sync_after_rotation INTEGER DEFAULT 0,last_ip TEXT,last_sync_status TEXT,last_sync_error TEXT,last_sync_at TEXT,created_at TEXT,updated_at TEXT);
CREATE TABLE IF NOT EXISTS rotations (id INTEGER PRIMARY KEY AUTOINCREMENT,machine_id TEXT NOT NULL,old_ip TEXT,new_ip TEXT,trigger_type TEXT,reason TEXT,status TEXT,dns_sync_status TEXT,post_check_status TEXT,error TEXT,started_at TEXT,finished_at TEXT);
CREATE TABLE IF NOT EXISTS logs (id INTEGER PRIMARY KEY AUTOINCREMENT,level TEXT,machine_id TEXT,job_type TEXT,message TEXT,meta_json TEXT,created_at TEXT);
CREATE TABLE IF NOT EXISTS app_settings (key TEXT PRIMARY KEY,value TEXT NOT NULL,updated_at TEXT);
CREATE INDEX IF NOT EXISTS idx_logs_created ON logs(created_at DESC); CREATE INDEX IF NOT EXISTS idx_probes_machine ON probe_configs(machine_id);
`
