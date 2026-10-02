// Package store 提供 SQLite 持久化：数据库= server 后台所有表与查询。
// 余额（users.balance）与积分流水（credit_records）在同一事务里更新，保证一致。
package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// ErrNotFound 通用“记录不存在”。
var ErrNotFound = errors.New("记录不存在")

// ReasonDailyClaim 每日积分领取在流水里的原因标记。
const ReasonDailyClaim = "daily_claim"

type User struct {
	ID        int64     `json:"id"`
	Phone     string    `json:"phone"`
	IsAdmin   bool      `json:"is_admin"`
	Balance   int64     `json:"balance"`
	Agreement bool      `json:"agreement"`
	CreatedAt time.Time `json:"created_at"`
}

type CreditRecord struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Amount    int64  `json:"amount"` // 正=赠送/回退，负=消耗
	Reason    string `json:"reason"`
	Model     string `json:"model,omitempty"`
	CreatedAt string `json:"created_at"`
	// ExpiresAt 到期时间（RFC3339，UTC）；空=永久。仅「获得」行有意义。
	ExpiresAt string `json:"expires_at,omitempty"`
	// Remaining 该笔「获得」的剩余可用积分；消耗行恒 0。
	Remaining int64 `json:"remaining"`
}

// ProviderModel 供应商下单个模型的能力配置（对齐桌面端：上下文/最大输出/输入类型/推理等级）。
type ProviderModel struct {
	Key             string   `json:"key"`
	ContextWindow   int      `json:"context_window,omitempty"`
	MaxOutputTokens int      `json:"max_output_tokens,omitempty"`
	InputTypes      []string `json:"input_types,omitempty"`
	Reasoning       string   `json:"reasoning,omitempty"`
}

type Provider struct {
	ID        int64           `json:"id"`
	Name      string          `json:"name"`
	Type      string          `json:"type"` // openai/anthropic/gemini/ollama
	Model     string          `json:"model,omitempty"`
	Models    []ProviderModel `json:"models,omitempty"`
	BaseURL   string          `json:"base_url,omitempty"`
	APIKey    string          `json:"api_key,omitempty"`
	Enabled   bool            `json:"enabled"`
	CreatedAt time.Time       `json:"-"` // 服务端维护，不参与 API 收发
}

type Model struct {
	ID         int64     `json:"id"`
	Key        string    `json:"key"`     // 模型名（全局唯一），对应 gateway 收到的 model
	Builtin    bool      `json:"builtin"` // 内置模型：仅供网关/系统使用，不绑具体供应商 key 时走默认
	Enabled    bool      `json:"enabled"`
	Multiplier float64   `json:"multiplier"` // 倍率，默认 1
	Free       bool      `json:"free"`       // 限免：网关按 0 计费，界面显示 0x + 原倍率中划线
	CreatedAt  time.Time `json:"created_at"`
}

type ModelProvider struct {
	ModelID    int64 `json:"model_id"`
	ProviderID int64 `json:"provider_id"`
	Enabled    bool  `json:"enabled"`
	Priority   int   `json:"priority"`
}

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			phone TEXT NOT NULL UNIQUE,
			is_admin INTEGER NOT NULL DEFAULT 0,
			balance INTEGER NOT NULL DEFAULT 0,
			agreement INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS tokens (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			token TEXT NOT NULL UNIQUE,
			expires_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS credit_records (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			amount INTEGER NOT NULL,
			reason TEXT NOT NULL,
			model TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			expires_at TEXT NOT NULL DEFAULT '',
			remaining INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			type TEXT NOT NULL,
			model TEXT NOT NULL DEFAULT '',
			models TEXT NOT NULL DEFAULT '',
			base_url TEXT NOT NULL DEFAULT '',
			api_key TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS models (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			key TEXT NOT NULL UNIQUE,
			builtin INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			multiplier REAL NOT NULL DEFAULT 1,
			free INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS model_providers (
			model_id INTEGER NOT NULL,
			provider_id INTEGER NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (model_id, provider_id)
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS daily_claims (
			user_id INTEGER NOT NULL,
			claim_date TEXT NOT NULL,
			PRIMARY KEY (user_id, claim_date)
		)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	// 种子：内置管理账户（手机号 10000，管理员）+ 几个内置模型
	if err := s.ensureProviderColumns(); err != nil {
		return err
	}
	if err := s.ensureCreditColumns(); err != nil {
		return err
	}
	return s.seedDefaults()
}

// ensureCreditColumns 升级旧库：credit_records 补 expires_at/remaining 列，
// 并把历史「获得」行的 remaining 回填为 amount（旧库无有效期，余额口径不变）。
func (s *Store) ensureCreditColumns() error {
	cols := map[string]bool{}
	rows, err := s.db.Query(`PRAGMA table_info(credit_records)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		cols[name] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !cols["expires_at"] {
		if _, err := s.db.Exec(`ALTER TABLE credit_records ADD COLUMN expires_at TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["remaining"] {
		if _, err := s.db.Exec(`ALTER TABLE credit_records ADD COLUMN remaining INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
		// 回填：历史获得行剩余=全额，消耗行 0（余额=未到期剩余之和 的旧库等价）
		if _, err := s.db.Exec(`UPDATE credit_records SET remaining=amount WHERE amount>0`); err != nil {
			return err
		}
	}
	return nil
}

// ensureProviderColumns 升级旧库：providers 表补 model/models 列。
func (s *Store) ensureProviderColumns() error {
	cols := map[string]bool{}
	rows, err := s.db.Query(`PRAGMA table_info(providers)`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt any
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		cols[name] = true
	}
	if !cols["model"] {
		if _, err := s.db.Exec(`ALTER TABLE providers ADD COLUMN model TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	if !cols["models"] {
		if _, err := s.db.Exec(`ALTER TABLE providers ADD COLUMN models TEXT NOT NULL DEFAULT ''`); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) seedDefaults() error {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	if n == 0 {
		if _, err := s.db.Exec(
			`INSERT INTO users(phone, is_admin, balance, agreement, created_at) VALUES('10000', 1, 0, 1, ?)`,
			time.Now().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	var mn int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM models`).Scan(&mn)
	if mn == 0 {
		now := time.Now().Format(time.RFC3339)
		for _, key := range []string{"gpt-4o", "claude-sonnet-4-20250514", "gemini-2.5-pro"} {
			if _, err := s.db.Exec(
				`INSERT INTO models(key, builtin, enabled, multiplier, free, created_at) VALUES(?, 1, 1, 1, 0, ?)`,
				key, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func nowStr() string { return time.Now().Format(time.RFC3339) }

// nowUTC 到期时间统一按 UTC 存，字符串比较即时间先后。
func nowUTC() string { return time.Now().UTC().Format(time.RFC3339) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// ---------- 用户 ----------

func (s *Store) UserByPhone(phone string) (*User, error) {
	u := &User{}
	var created string
	err := s.db.QueryRow(
		`SELECT id, phone, is_admin, balance, agreement, created_at FROM users WHERE phone=?`, phone).
		Scan(&u.ID, &u.Phone, &u.IsAdmin, &u.Balance, &u.Agreement, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt = parseTime(created)
	return u, nil
}

func (s *Store) UserByID(id int64) (*User, error) {
	u := &User{}
	var created string
	err := s.db.QueryRow(
		`SELECT id, phone, is_admin, balance, agreement, created_at FROM users WHERE id=?`, id).
		Scan(&u.ID, &u.Phone, &u.IsAdmin, &u.Balance, &u.Agreement, &created)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt = parseTime(created)
	return u, nil
}

// UpdateUserPhone 管理后台修改用户手机号（唯一性校验）。
func (s *Store) UpdateUserPhone(id int64, phone string) error {
	var cnt int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE phone=? AND id<>?`, phone, id).Scan(&cnt); err != nil {
		return err
	}
	if cnt > 0 {
		return errors.New("该手机号已被占用")
	}
	_, err := s.db.Exec(`UPDATE users SET phone=? WHERE id=?`, phone, id)
	return err
}

// CreateUser 新建用户（自动注册）。
func (s *Store) CreateUser(phone string) (*User, error) {
	res, err := s.db.Exec(`INSERT INTO users(phone, agreement, created_at) VALUES(?, 0, ?)`,
		phone, nowStr())
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.UserByID(id)
}

// AcceptAgreement 用户账号标记已同意协议。
func (s *Store) AcceptAgreement(id int64) error {
	_, err := s.db.Exec(`UPDATE users SET agreement=1 WHERE id=?`, id)
	return err
}

// sweepExpired 把已到期未耗尽的「获得」置失效（remaining=0）并同步扣减余额，
// 保证余额恒等于「未到期剩余之和」。在事务内调用。
func sweepExpired(tx *sql.Tx, userID int64, now string) error {
	var lost int64
	if err := tx.QueryRow(
		`SELECT COALESCE(SUM(remaining),0) FROM credit_records
		 WHERE user_id=? AND expires_at!='' AND expires_at<? AND remaining>0`,
		userID, now).Scan(&lost); err != nil {
		return err
	}
	if lost == 0 {
		return nil
	}
	if _, err := tx.Exec(
		`UPDATE credit_records SET remaining=0
		 WHERE user_id=? AND expires_at!='' AND expires_at<? AND remaining>0`,
		userID, now); err != nil {
		return err
	}
	_, err := tx.Exec(`UPDATE users SET balance=balance-? WHERE id=?`, lost, userID)
	return err
}

// SweepExpired 对该用户执行一次到期清理（读路径用，保证展示的余额已剔除过期积分）。
func (s *Store) SweepExpired(userID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := sweepExpired(tx, userID, nowUTC()); err != nil {
		return err
	}
	return tx.Commit()
}

// AddCredit 增减积分并写流水（事务保证余额与流水一致）。返回新余额。
// expiresAt 为该笔「获得」的到期时间（RFC3339 UTC，空=永久）；
// 扣减（delta<0）按 FEFO：最快到期的先用，无到期时间的最后用。
func (s *Store) AddCredit(userID int64, delta int64, reason, model, expiresAt string) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	now := nowUTC()
	if err := sweepExpired(tx, userID, now); err != nil {
		return 0, err
	}

	var bal int64
	if err := tx.QueryRow(`SELECT balance FROM users WHERE id=?`, userID).Scan(&bal); err != nil {
		return 0, err
	}
	if delta >= 0 {
		bal += delta
		if _, err := tx.Exec(`UPDATE users SET balance=? WHERE id=?`, bal, userID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(
			`INSERT INTO credit_records(user_id, amount, reason, model, created_at, expires_at, remaining) VALUES(?, ?, ?, ?, ?, ?, ?)`,
			userID, delta, reason, model, nowStr(), expiresAt, delta); err != nil {
			return 0, err
		}
	} else {
		// FEFO：最快到期先扣，无到期时间（expires_at=''）排最后
		need := -delta
		rows, err := tx.Query(
			`SELECT id, remaining FROM credit_records
			 WHERE user_id=? AND remaining>0 AND (expires_at='' OR expires_at>=?)
			 ORDER BY (expires_at=''), expires_at ASC`, userID, now)
		if err != nil {
			return 0, err
		}
		type take struct {
			id, amount int64
		}
		var takes []take
		for rows.Next() && need > 0 {
			var id, rem int64
			if err := rows.Scan(&id, &rem); err != nil {
				rows.Close()
				return 0, err
			}
			use := rem
			if use > need {
				use = need
			}
			takes = append(takes, take{id, use})
			need -= use
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, err
		}
		if need > 0 {
			return 0, errors.New("积分不足")
		}
		for _, tk := range takes {
			if _, err := tx.Exec(`UPDATE credit_records SET remaining=remaining-? WHERE id=?`, tk.amount, tk.id); err != nil {
				return 0, err
			}
		}
		bal += delta
		if _, err := tx.Exec(`UPDATE users SET balance=? WHERE id=?`, bal, userID); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(
			`INSERT INTO credit_records(user_id, amount, reason, model, created_at, expires_at, remaining) VALUES(?, ?, ?, ?, ?, '', 0)`,
			userID, delta, reason, model, nowStr()); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return bal, nil
}

// Records 用户积分流水（倒序）。
func (s *Store) Records(userID int64, limit int) ([]CreditRecord, error) {
	if err := s.SweepExpired(userID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(
		`SELECT id, user_id, amount, reason, model, created_at, expires_at, remaining FROM credit_records WHERE user_id=? ORDER BY id DESC LIMIT ?`,
		userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out = []CreditRecord{}
	for rows.Next() {
		var r CreditRecord
		if err := rows.Scan(&r.ID, &r.UserID, &r.Amount, &r.Reason, &r.Model, &r.CreatedAt, &r.ExpiresAt, &r.Remaining); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(
		`SELECT id, phone, is_admin, balance, agreement, created_at FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out = []*User{}
	for rows.Next() {
		var u User
		var created string
		if err := rows.Scan(&u.ID, &u.Phone, &u.IsAdmin, &u.Balance, &u.Agreement, &created); err != nil {
			return nil, err
		}
		u.CreatedAt = parseTime(created)
		out = append(out, &u)
	}
	return out, rows.Err()
}

// ---------- Token ----------

func NewToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) CreateToken(userID int64, ttl time.Duration) (string, error) {
	token := NewToken()
	exp := time.Now().Add(ttl).Format(time.RFC3339)
	_, err := s.db.Exec(`INSERT INTO tokens(user_id, token, expires_at) VALUES(?, ?, ?)`, userID, token, exp)
	return token, err
}

// UserByToken 按 token 取有效用户。
func (s *Store) UserByToken(token string) (*User, error) {
	var userID int64
	var exp string
	err := s.db.QueryRow(`SELECT user_id, expires_at FROM tokens WHERE token=?`, token).Scan(&userID, &exp)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if t := parseTime(exp); !t.IsZero() && t.Before(time.Now()) {
		return nil, nil // 过期
	}
	return s.UserByID(userID)
}

func (s *Store) DeleteToken(token string) error {
	_, err := s.db.Exec(`DELETE FROM tokens WHERE token=?`, token)
	return err
}

// ---------- 供应商 ----------

func (s *Store) ListProviders() ([]*Provider, error) {
	rows, err := s.db.Query(
		`SELECT id, name, type, model, models, base_url, api_key, enabled, created_at FROM providers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out = []*Provider{}
	for rows.Next() {
		var p Provider
		var created, modelsJSON string
		if err := rows.Scan(&p.ID, &p.Name, &p.Type, &p.Model, &modelsJSON, &p.BaseURL, &p.APIKey, &p.Enabled, &created); err != nil {
			return nil, err
		}
		p.CreatedAt = parseTime(created)
		if modelsJSON != "" {
			_ = json.Unmarshal([]byte(modelsJSON), &p.Models)
		}
		if p.Models == nil {
			p.Models = []ProviderModel{}
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

func (s *Store) SaveProvider(p *Provider) error {
	modelsJSON, err := json.Marshal(p.Models)
	if err != nil {
		return err
	}
	if p.Models == nil {
		modelsJSON = []byte("[]")
	}
	if p.ID == 0 {
		res, err := s.db.Exec(
			`INSERT INTO providers(name, type, model, models, base_url, api_key, enabled, created_at) VALUES(?,?,?,?,?,?,?,?)`,
			p.Name, p.Type, p.Model, string(modelsJSON), p.BaseURL, p.APIKey, boolInt(p.Enabled), nowStr())
		if err != nil {
			return err
		}
		p.ID, _ = res.LastInsertId()
		return nil
	}
	_, err = s.db.Exec(
		`UPDATE providers SET name=?, type=?, model=?, models=?, base_url=?, api_key=?, enabled=? WHERE id=?`,
		p.Name, p.Type, p.Model, string(modelsJSON), p.BaseURL, p.APIKey, boolInt(p.Enabled), p.ID)
	return err
}

func (s *Store) DeleteProvider(id int64) error {
	if _, err := s.db.Exec(`DELETE FROM providers WHERE id=?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM model_providers WHERE provider_id=?`, id)
	return err
}

// SyncProviderModels 把供应商「模型配置」里的模型自动同步进 models 表并建绑定：
// 用户在后台配好供应商的模型，网关侧即可直接选用，无需再到「模型」里手工添加。
// 已存在的模型不覆盖其倍率/启停设置，只补绑定。
func (s *Store) SyncProviderModels(providerID int64, models []ProviderModel) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, m := range models {
		key := strings.TrimSpace(m.Key)
		if key == "" {
			continue
		}
		var id int64
		err := tx.QueryRow(`SELECT id FROM models WHERE key=?`, key).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			res, err := tx.Exec(
				`INSERT INTO models(key, builtin, enabled, multiplier, free, created_at) VALUES(?,0,1,1,0,?)`,
				key, nowStr())
			if err != nil {
				return err
			}
			id, _ = res.LastInsertId()
		} else if err != nil {
			return err
		}
		var cnt int
		_ = tx.QueryRow(`SELECT COUNT(*) FROM model_providers WHERE model_id=? AND provider_id=?`, id, providerID).Scan(&cnt)
		if cnt == 0 {
			if _, err := tx.Exec(
				`INSERT INTO model_providers(model_id, provider_id, enabled, priority) VALUES(?,?,1,0)`,
				id, providerID); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// ---------- 模型 ----------

func (s *Store) ListModels() ([]*Model, error) {
	rows, err := s.db.Query(`SELECT id, key, builtin, enabled, multiplier, free, created_at FROM models ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out = []*Model{}
	for rows.Next() {
		var m Model
		var created string
		if err := rows.Scan(&m.ID, &m.Key, &m.Builtin, &m.Enabled, &m.Multiplier, &m.Free, &created); err != nil {
			return nil, err
		}
		m.CreatedAt = parseTime(created)
		out = append(out, &m)
	}
	return out, rows.Err()
}

func (s *Store) ModelByKey(key string) (*Model, error) {
	m := &Model{}
	var created string
	err := s.db.QueryRow(
		`SELECT id, key, builtin, enabled, multiplier, free, created_at FROM models WHERE key=?`, key).
		Scan(&m.ID, &m.Key, &m.Builtin, &m.Enabled, &m.Multiplier, &m.Free, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.CreatedAt = parseTime(created)
	return m, nil
}

func (s *Store) SaveModel(m *Model, providerID int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if m.ID == 0 {
		res, err := tx.Exec(
			`INSERT INTO models(key, builtin, enabled, multiplier, free, created_at) VALUES(?,?,?,?,?,?)`,
			m.Key, boolInt(m.Builtin), boolInt(m.Enabled), m.Multiplier, boolInt(m.Free), nowStr())
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		m.ID = id
	} else {
		if _, err := tx.Exec(
			`UPDATE models SET key=?, enabled=?, multiplier=?, free=? WHERE id=?`,
			m.Key, boolInt(m.Enabled), m.Multiplier, boolInt(m.Free), m.ID); err != nil {
			return err
		}
	}
	// 绑定供应商（非内置模型）：一个模型可绑多个供应商，这里加一条归属
	if providerID > 0 {
		var cnt int
		_ = tx.QueryRow(`SELECT COUNT(*) FROM model_providers WHERE model_id=? AND provider_id=?`, m.ID, providerID).
			Scan(&cnt)
		if cnt == 0 {
			_, err = tx.Exec(
				`INSERT INTO model_providers(model_id, provider_id, enabled, priority) VALUES(?,?,1,0)`,
				m.ID, providerID)
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteModel(id int64) error {
	if _, err := s.db.Exec(`DELETE FROM model_providers WHERE model_id=?`, id); err != nil {
		return err
	}
	_, err := s.db.Exec(`DELETE FROM models WHERE id=?`, id)
	return err
}

// ModelProviders 某模型绑定的供应商列表（控制器多供应商路由/启停）。
func (s *Store) ModelProviders(modelID int64) ([]ModelProvider, error) {
	rows, err := s.db.Query(
		`SELECT model_id, provider_id, enabled, priority FROM model_providers WHERE model_id=? ORDER BY priority, provider_id`,
		modelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out = []ModelProvider{}
	for rows.Next() {
		var mp ModelProvider
		if err := rows.Scan(&mp.ModelID, &mp.ProviderID, &mp.Enabled, &mp.Priority); err != nil {
			return nil, err
		}
		out = append(out, mp)
	}
	return out, rows.Err()
}

// AllModelProviders 全部模型×供应商关系，供管理页整体展示。
func (s *Store) AllModelProviders() ([]ModelProvider, error) {
	rows, err := s.db.Query(
		`SELECT model_id, provider_id, enabled, priority FROM model_providers ORDER BY model_id, priority, provider_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out = []ModelProvider{}
	for rows.Next() {
		var mp ModelProvider
		if err := rows.Scan(&mp.ModelID, &mp.ProviderID, &mp.Enabled, &mp.Priority); err != nil {
			return nil, err
		}
		out = append(out, mp)
	}
	return out, rows.Err()
}

// ToggleModelProviderEnabled 启/停某模型下的某供应商。
func (s *Store) ToggleModelProviderEnabled(modelID, providerID int64, enabled bool) error {
	_, err := s.db.Exec(
		`UPDATE model_providers SET enabled=? WHERE model_id=? AND provider_id=?`,
		boolInt(enabled), modelID, providerID)
	return err
}

// ProviderModelCfg 网关转发某模型时使用的能力配置（上下文/最大输出/输入类型/推理等级）。
type ProviderModelCfg struct {
	Provider      *Provider
	UpstreamModel string // 上游真正使用的模型名（provider.Model）
	ContextWindow int
	MaxOutput     int
	InputTypes    []string
	Reasoning     string
}

// UpstreamsForModel 网关路由：某模型可用的启用供应商（按优先级升序：model_providers.priority）。
// 无 model_providers 绑定时退回「供应商自身 Model/Models 里出现该 key」的启用供应商。
func (s *Store) UpstreamsForModel(key string) ([]*ProviderModelCfg, error) {
	models, err := s.ListModels()
	if err != nil {
		return nil, err
	}
	providers, err := s.ListProviders()
	if err != nil {
		return nil, err
	}
	var target *Model
	for _, m := range models {
		if m.Key == key {
			target = m
		}
	}

	cfgFor := func(p *Provider) *ProviderModelCfg {
		upModel := p.Model
		var c ProviderModelCfg
		c.Provider = p
		for _, pm := range p.Models {
			if pm.Key == key {
				c.UpstreamModel = pm.Key
				c.ContextWindow = pm.ContextWindow
				c.MaxOutput = pm.MaxOutputTokens
				c.InputTypes = pm.InputTypes
				c.Reasoning = pm.Reasoning
				break
			}
		}
		_ = upModel
		return &c
	}

	byProvider := map[int64]*Provider{}
	var byID []int64
	for _, p := range providers {
		if !p.Enabled {
			continue
		}
		byProvider[p.ID] = p
		byID = append(byID, p.ID)
	}

	// 优先：模型在 model_providers 里的启用关系
	if target != nil {
		mps, _ := s.ModelProviders(target.ID)
		var ids []int64
		for _, mp := range mps {
			if mp.Enabled {
				ids = append(ids, mp.ProviderID)
			}
		}
		if len(ids) > 0 {
			out := make([]*ProviderModelCfg, 0, len(ids))
			for _, pid := range ids {
				if p, ok := byProvider[pid]; ok {
					out = append(out, cfgFor(p))
				}
			}
			return out, nil
		}
	}

	// 退回：供应商自身声明提供该模型
	var out []*ProviderModelCfg
	for _, pid := range byID {
		p := byProvider[pid]
		if p.Model == key {
			out = append(out, cfgFor(p))
			continue
		}
		for _, pm := range p.Models {
			if pm.Key == key {
				out = append(out, cfgFor(p))
				break
			}
		}
	}
	return out, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ---------- 系统设置 / 每日积分 ----------

// GetSetting 取系统设置值，不存在返回空串。
func (s *Store) GetSetting(key string) string {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM settings WHERE key=?`, key).Scan(&v); err != nil {
		return ""
	}
	return v
}

// SetSetting 存系统设置（upsert）。
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings(key, value) VALUES(?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// ClaimDaily 每日积分领取：同一用户同一天幂等（重复调用不再发放）。
// validDays>0 时带有效期（自领取之日计算，到期自动失效）：最近一次领取未满
// validDays 天不再发放，满期当天起可重新领取。余额更新与流水写入同一事务。
func (s *Store) ClaimDaily(userID, amount, validDays int64) (claimed bool, balance int64, err error) {
	if amount <= 0 {
		return false, 0, errors.New("积分数量无效")
	}
	today := time.Now().Format("2006-01-02")
	tx, err := s.db.Begin()
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback()

	var bal int64
	if err := tx.QueryRow(`SELECT balance FROM users WHERE id=?`, userID).Scan(&bal); err != nil {
		return false, 0, err
	}
	// 有效期未到：距最近一次领取不足 validDays 天，不发放（自动失效由时间天然完成）
	if validDays > 0 {
		var last string
		if err := tx.QueryRow(
			`SELECT claim_date FROM daily_claims WHERE user_id=? ORDER BY claim_date DESC LIMIT 1`, userID,
		).Scan(&last); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return false, 0, err
		}
		if d, perr := time.Parse("2006-01-02", last); perr == nil {
			if !d.AddDate(0, 0, int(validDays)).Before(time.Now()) {
				return false, bal, tx.Commit()
			}
		}
	}
	var cnt int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM daily_claims WHERE user_id=? AND claim_date=?`, userID, today).Scan(&cnt); err != nil {
		return false, 0, err
	}
	if cnt > 0 {
		return false, bal, tx.Commit()
	}
	if _, err := tx.Exec(`INSERT INTO daily_claims(user_id, claim_date) VALUES(?, ?)`, userID, today); err != nil {
		return false, 0, err
	}
	// 有效期：validDays>0 时该笔积分 validDays 天后失效，消耗与扣减都按到期时间走
	expiresAt := ""
	if validDays > 0 {
		expiresAt = time.Now().AddDate(0, 0, int(validDays)).UTC().Format(time.RFC3339)
	}
	bal += amount
	if _, err := tx.Exec(`UPDATE users SET balance=? WHERE id=?`, bal, userID); err != nil {
		return false, 0, err
	}
	if _, err := tx.Exec(
		`INSERT INTO credit_records(user_id, amount, reason, model, created_at, expires_at, remaining) VALUES(?, ?, ?, '', ?, ?, ?)`,
		userID, amount, ReasonDailyClaim, nowStr(), expiresAt, amount); err != nil {
		return false, 0, err
	}
	if err := tx.Commit(); err != nil {
		return false, 0, err
	}
	return true, bal, nil
}

// EnabledModels 全部启用中的模型（前端「内置模型」组数据源）。
func (s *Store) EnabledModels() ([]*Model, error) {
	rows, err := s.db.Query(`SELECT id, key, builtin, enabled, multiplier, free, created_at FROM models WHERE enabled=1 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out = []*Model{}
	for rows.Next() {
		var m Model
		var created string
		if err := rows.Scan(&m.ID, &m.Key, &m.Builtin, &m.Enabled, &m.Multiplier, &m.Free, &created); err != nil {
			return nil, err
		}
		m.CreatedAt = parseTime(created)
		out = append(out, &m)
	}
	return out, rows.Err()
}
