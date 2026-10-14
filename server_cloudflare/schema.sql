-- OmniAgnes 平台服务端 · D1 建表。
-- 与 Go 版（server/internal/store/store.go 的 migrate）逐字段一致；多一张 ide_pairs
-- （一次性授权码——Go 版存内存，Workers 无常驻内存，落库；取出即删保证一次性）。
-- 执行：wrangler d1 execute omni_server --remote --file=./schema.sql

CREATE TABLE IF NOT EXISTS users (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    name          TEXT NOT NULL DEFAULT '',
    email         TEXT NOT NULL UNIQUE,
    password      TEXT NOT NULL DEFAULT '',
    role          TEXT NOT NULL DEFAULT 'member',
    points        INTEGER NOT NULL DEFAULT 0,
    status        TEXT NOT NULL DEFAULT 'active',
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS personal_access_tokens (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL DEFAULT '',
    token_hash   TEXT NOT NULL UNIQUE,
    ip_address   TEXT NOT NULL DEFAULT '',
    user_agent   TEXT NOT NULL DEFAULT '',
    expires_at   TEXT,
    created_at   TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS login_logs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    ip_address  TEXT NOT NULL DEFAULT '',
    device      TEXT NOT NULL DEFAULT '',
    user_agent  TEXT NOT NULL DEFAULT '',
    login_at    TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

-- 一台 omni 桌面端 = 一行。token 进二维码，secret 只给桌面端自己。
CREATE TABLE IF NOT EXISTS ide_devices (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id         INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            TEXT NOT NULL DEFAULT '',
    platform        TEXT NOT NULL DEFAULT '',
    token           TEXT NOT NULL UNIQUE,
    secret          TEXT NOT NULL DEFAULT '',
    lan_url         TEXT NOT NULL DEFAULT '',
    enabled         INTEGER NOT NULL DEFAULT 1,
    last_seen_at    TEXT,
    mobile_seen_at  TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ide_devices_user ON ide_devices(user_id, enabled);

-- 手机下发的一条指令的命运流水。
CREATE TABLE IF NOT EXISTS ide_rpcs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id    INTEGER NOT NULL REFERENCES ide_devices(id) ON DELETE CASCADE,
    method       TEXT NOT NULL DEFAULT '',
    params       TEXT,
    status       TEXT NOT NULL DEFAULT 'pending',
    result       TEXT,
    error        TEXT,
    picked_at    TEXT,
    finished_at  TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ide_rpcs_device_status ON ide_rpcs(device_id, status);

-- 桌面端推上来的内核事件，手机按自增 id 作游标增量拉取。只留 5 分钟（写入时顺手清）。
CREATE TABLE IF NOT EXISTS ide_events (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id   INTEGER NOT NULL REFERENCES ide_devices(id) ON DELETE CASCADE,
    type        TEXT NOT NULL DEFAULT '',
    payload     TEXT,
    created_at  TEXT
);
CREATE INDEX IF NOT EXISTS idx_ide_events_device_id ON ide_events(device_id, id);

-- 一次性授权码（网页登录 → 桌面端换 token）。10 分钟过期，取出即删。
CREATE TABLE IF NOT EXISTS ide_pairs (
    code        TEXT PRIMARY KEY,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device      TEXT,
    expires_at  TEXT NOT NULL
);

-- 模型库：对外可见的「模型名」清单。
CREATE TABLE IF NOT EXISTS ai_models (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,
    builtin     INTEGER NOT NULL DEFAULT 0,
    ratio       REAL NOT NULL DEFAULT 1,
    enabled     INTEGER NOT NULL DEFAULT 1,
    remark      TEXT NOT NULL DEFAULT '',
    sort        INTEGER NOT NULL DEFAULT 0,
    free        INTEGER NOT NULL DEFAULT 0,
    discount    REAL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

-- 上游：一个 OpenAI 兼容服务。
CREATE TABLE IF NOT EXISTS ai_upstreams (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT NOT NULL DEFAULT '',
    driver           TEXT NOT NULL DEFAULT 'openai',
    base_url         TEXT NOT NULL DEFAULT '',
    enabled          INTEGER NOT NULL DEFAULT 1,
    sort             INTEGER NOT NULL DEFAULT 0,
    remark           TEXT NOT NULL DEFAULT '',
    models_synced_at TEXT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

-- 上游 API Key：一个上游可配多把，网关按状态择优、失败自动切换。
CREATE TABLE IF NOT EXISTS ai_upstream_keys (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    upstream_id     INTEGER NOT NULL REFERENCES ai_upstreams(id) ON DELETE CASCADE,
    name            TEXT NOT NULL DEFAULT '',
    api_key         TEXT NOT NULL DEFAULT '',
    enabled         INTEGER NOT NULL DEFAULT 1,
    status          TEXT NOT NULL DEFAULT 'unknown',
    latency_ms      INTEGER,
    last_error      TEXT,
    last_checked_at TEXT,
    last_used_at    TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

-- 上游自动获取到的模型。model_id 为空表示还没匹配到模型库。
CREATE TABLE IF NOT EXISTS ai_upstream_models (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    upstream_id     INTEGER NOT NULL REFERENCES ai_upstreams(id) ON DELETE CASCADE,
    model_id        INTEGER REFERENCES ai_models(id) ON DELETE SET NULL,
    model           TEXT NOT NULL DEFAULT '',
    enabled         INTEGER NOT NULL DEFAULT 1,
    status          TEXT NOT NULL DEFAULT 'unknown',
    latency_ms      INTEGER,
    last_error      TEXT,
    last_checked_at TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_aum_upstream ON ai_upstream_models(upstream_id);
CREATE INDEX IF NOT EXISTS idx_aum_model ON ai_upstream_models(model_id);

-- 一次网关调用的用量流水。
CREATE TABLE IF NOT EXISTS ai_usages (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id           INTEGER REFERENCES users(id) ON DELETE SET NULL,
    device_id         INTEGER REFERENCES ide_devices(id) ON DELETE SET NULL,
    model             TEXT NOT NULL DEFAULT '',
    ratio             REAL NOT NULL DEFAULT 1,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens      INTEGER NOT NULL DEFAULT 0,
    points            INTEGER NOT NULL DEFAULT 0,
    source            TEXT NOT NULL DEFAULT 'ide',
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ai_usages_user ON ai_usages(user_id, created_at);

-- 积分账本流水。
CREATE TABLE IF NOT EXISTS point_logs (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    change       INTEGER NOT NULL DEFAULT 0,
    after        INTEGER NOT NULL DEFAULT 0,
    type         TEXT NOT NULL DEFAULT '',
    remark       TEXT NOT NULL DEFAULT '',
    operator_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL
);

-- 模型健康探测明细与按天聚合。
CREATE TABLE IF NOT EXISTS ai_model_health_checks (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    upstream_model_id INTEGER NOT NULL REFERENCES ai_upstream_models(id) ON DELETE CASCADE,
    ok                INTEGER NOT NULL DEFAULT 0,
    latency_ms        INTEGER,
    error             TEXT,
    created_at        TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ai_model_health_days (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    model_id   INTEGER NOT NULL REFERENCES ai_models(id) ON DELETE CASCADE,
    day        TEXT NOT NULL,
    total      INTEGER NOT NULL DEFAULT 0,
    ok         INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    UNIQUE(model_id, day)
);

-- 系统配置 KV。
CREATE TABLE IF NOT EXISTS settings (
    key         TEXT PRIMARY KEY,
    value       TEXT,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

-- 邮箱验证码（注册 / 验证码登录 / 超管换邮箱）：email+purpose 一行，验证通过或作废时删除；
-- attempts 防暴力试码（错 5 次作废），created_at 兼作 60 秒重发节流的依据。
CREATE TABLE IF NOT EXISTS mail_codes (
    email       TEXT NOT NULL,
    purpose     TEXT NOT NULL,
    code        TEXT NOT NULL,
    attempts    INTEGER NOT NULL DEFAULT 0,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    PRIMARY KEY (email, purpose)
);

-- ========== 桌面端兼容层（desk_*）==========
-- omni 桌面端（webui/frontend）说的是 Go 版 server/ 的协议：手机号+验证码登录、
-- balance 计积分、积分按笔带到期时间与剩余量（FEFO 扣减）。与本文件上方「邮箱账号
-- + users.points + point_logs」语义不同，故独立建表，互不掺混：
-- 桌面端账号不会被后台的会员/积分逻辑读到，反之亦然。逐字段对齐 store.go 的 migrate()。
-- 模型不另建表：GET /api/models 与计费倍率直接读后台已配的 ai_models（name→key、ratio→multiplier）。

CREATE TABLE IF NOT EXISTS desk_users (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    phone       TEXT NOT NULL UNIQUE,
    is_admin    INTEGER NOT NULL DEFAULT 0,
    balance     INTEGER NOT NULL DEFAULT 0,
    agreement   INTEGER NOT NULL DEFAULT 0,
    created_at  TEXT NOT NULL
);

-- 桌面端登录态：30 天有效。明文只进响应，库里存 SHA-256（与 personal_access_tokens 同法）。
CREATE TABLE IF NOT EXISTS desk_tokens (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES desk_users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

-- 积分账本：amount 正=获得、负=消耗。获得行带 expires_at（空=永久）与 remaining（剩余可用），
-- 消耗行 remaining 恒 0。余额 = 未过期获得行的 remaining 之和，由到期清扫维护。
CREATE TABLE IF NOT EXISTS desk_credit_records (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES desk_users(id) ON DELETE CASCADE,
    amount      INTEGER NOT NULL,
    reason      TEXT NOT NULL,
    model       TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    expires_at  TEXT NOT NULL DEFAULT '',
    remaining   INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_dcr_user ON desk_credit_records(user_id, amount);
CREATE INDEX IF NOT EXISTS idx_dcr_user_remaining ON desk_credit_records(user_id, remaining, expires_at);

-- 一次桌面端 AI 请求的流水（status：ok=已计费 / insufficient=积分不足未扣 / error=请求失败不计费）。
CREATE TABLE IF NOT EXISTS desk_usage_logs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id         INTEGER NOT NULL REFERENCES desk_users(id) ON DELETE CASCADE,
    model           TEXT NOT NULL DEFAULT '',
    input_tokens    INTEGER NOT NULL DEFAULT 0,
    output_tokens   INTEGER NOT NULL DEFAULT 0,
    credits         INTEGER NOT NULL DEFAULT 0,
    status          TEXT NOT NULL DEFAULT 'ok',
    error           TEXT NOT NULL DEFAULT '',
    created_at      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_dul_user ON desk_usage_logs(user_id, id);

-- 每日领取去重（user+日期 一行）。
CREATE TABLE IF NOT EXISTS desk_daily_claims (
    user_id     INTEGER NOT NULL REFERENCES desk_users(id) ON DELETE CASCADE,
    claim_date  TEXT NOT NULL,
    PRIMARY KEY (user_id, claim_date)
);

-- 手机号验证码：Workers 无常驻内存，落库（Go 版在内存 OTPStore）。取出即删保证一次性。
CREATE TABLE IF NOT EXISTS desk_codes (
    phone       TEXT PRIMARY KEY,
    code        TEXT NOT NULL,
    attempts    INTEGER NOT NULL DEFAULT 0,
    expires_at  TEXT NOT NULL,
    created_at  TEXT NOT NULL
);

-- 不内置任何账号：desk_users 为空时第一个用手机号验证码登录的账号自动成为管理员
-- （见 src/desk.ts 的 isFirstUser），之后注册的都算 member。

