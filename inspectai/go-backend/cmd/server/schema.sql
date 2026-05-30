CREATE TABLE IF NOT EXISTS records (
    id                  TEXT PRIMARY KEY,
    project             TEXT NOT NULL,
    point_id            TEXT NOT NULL,
    point_name          TEXT NOT NULL,
    template_id         TEXT NOT NULL,
    template_name       TEXT NOT NULL,
    type                TEXT NOT NULL,
    inspector           TEXT NOT NULL,
    capture_attempts    INTEGER NOT NULL DEFAULT 0,
    manual_required     INTEGER NOT NULL DEFAULT 0,
    recognition_status  TEXT NOT NULL DEFAULT 'not_started',
    retake_reason       TEXT NOT NULL DEFAULT '',
    task_id             TEXT NOT NULL DEFAULT '',
    fields_json         TEXT NOT NULL DEFAULT '[]',
    images_json         TEXT NOT NULL DEFAULT '[]',
    report              TEXT NOT NULL DEFAULT '',
    ai_summary          TEXT NOT NULL DEFAULT '',
    ai_summary_tags     TEXT NOT NULL DEFAULT '[]',
    ai_recommendations  TEXT NOT NULL DEFAULT '[]',
    ai_summary_error    TEXT NOT NULL DEFAULT '',
    submitted           INTEGER NOT NULL DEFAULT 0,
    submitted_at        TEXT,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_records_created_at ON records(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_records_template_id ON records(template_id);

CREATE TABLE IF NOT EXISTS assets (
    id                  TEXT PRIMARY KEY,
    project_code        TEXT NOT NULL DEFAULT '',
    project             TEXT NOT NULL,
    point_id            TEXT NOT NULL DEFAULT '',
    template_id         TEXT NOT NULL DEFAULT '',
    asset_type          TEXT NOT NULL,
    asset_key           TEXT NOT NULL DEFAULT '',
    asset_name          TEXT NOT NULL,
    last_record_id      TEXT,
    last_status         TEXT NOT NULL DEFAULT '未巡检',
    status_level        TEXT NOT NULL DEFAULT 'unknown',
    status_order        INTEGER NOT NULL DEFAULT 99,
    last_summary        TEXT NOT NULL DEFAULT '',
    last_inspected_at   TEXT,
    last_inspector      TEXT NOT NULL DEFAULT '',
    last_photo_path     TEXT NOT NULL DEFAULT '',
    inspection_count    INTEGER NOT NULL DEFAULT 0,
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_assets_project ON assets(project, asset_type);
CREATE INDEX IF NOT EXISTS idx_assets_status ON assets(last_status, status_level);
CREATE INDEX IF NOT EXISTS idx_assets_project_status ON assets(project, last_status);
CREATE INDEX IF NOT EXISTS idx_assets_project_code ON assets(project_code, status_order);
CREATE INDEX IF NOT EXISTS idx_assets_asset_key ON assets(asset_key);
CREATE INDEX IF NOT EXISTS idx_assets_updated_at ON assets(updated_at);

CREATE TABLE IF NOT EXISTS ai_tasks (
    id                  TEXT PRIMARY KEY,
    record_id           TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'queued',
    progress_done       INTEGER NOT NULL DEFAULT 0,
    progress_total      INTEGER NOT NULL DEFAULT 0,
    error_code          TEXT NOT NULL DEFAULT '',
    error_message       TEXT NOT NULL DEFAULT '',
    analysis_json       TEXT NOT NULL DEFAULT '{}',
    created_at          TEXT NOT NULL,
    updated_at          TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_record_id ON ai_tasks(record_id, created_at DESC);

CREATE TABLE IF NOT EXISTS change_requests (
    id              TEXT PRIMARY KEY,
    target_type     TEXT NOT NULL,
    target_id       TEXT NOT NULL,
    patch_json      TEXT NOT NULL DEFAULT '{}',
    reason          TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'pending',
    requested_by    TEXT NOT NULL DEFAULT '',
    requested_at    TEXT NOT NULL,
    reviewed_by     TEXT,
    reviewed_at     TEXT,
    review_note     TEXT NOT NULL DEFAULT '',
    applied_at      TEXT
);

CREATE INDEX IF NOT EXISTS idx_cr_status ON change_requests(status, requested_at);
CREATE INDEX IF NOT EXISTS idx_cr_target ON change_requests(target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_cr_requester ON change_requests(requested_by, requested_at);

CREATE TABLE IF NOT EXISTS submission_idempotency (
    record_id   TEXT PRIMARY KEY,
    idem_key    TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'processing',
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_submission_idem_key ON submission_idempotency(idem_key);

CREATE TABLE IF NOT EXISTS roles (
    id          TEXT PRIMARY KEY,
    code        TEXT NOT NULL UNIQUE,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS departments (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    parent_id  TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_departments_parent ON departments(parent_id);

CREATE TABLE IF NOT EXISTS users (
    id              TEXT PRIMARY KEY,
    username        TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL,
    phone           TEXT NOT NULL DEFAULT '',
    avatar          TEXT NOT NULL DEFAULT '',
    role_id         TEXT NOT NULL,
    department_id   TEXT,
    wework_user_id  TEXT NOT NULL DEFAULT '',
    password_hash   TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'active',
    last_login_at   TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_users_role ON users(role_id);
CREATE INDEX IF NOT EXISTS idx_users_department ON users(department_id);
CREATE INDEX IF NOT EXISTS idx_users_wework ON users(wework_user_id);
CREATE INDEX IF NOT EXISTS idx_users_status ON users(status);

CREATE TABLE IF NOT EXISTS login_sessions (
    id          TEXT PRIMARY KEY,
    user_id     TEXT NOT NULL,
    token_hash  TEXT NOT NULL UNIQUE,
    expire_at   TEXT NOT NULL,
    created_at  TEXT NOT NULL,
    updated_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sessions_user ON login_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expire ON login_sessions(expire_at);

CREATE TABLE IF NOT EXISTS operation_logs (
    id          TEXT PRIMARY KEY,
    user_id     TEXT,
    actor_name  TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL,
    target_type TEXT NOT NULL DEFAULT '',
    target_id   TEXT NOT NULL DEFAULT '',
    detail_json TEXT NOT NULL DEFAULT '{}',
    created_at  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_operation_logs_user ON operation_logs(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_operation_logs_target ON operation_logs(target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_operation_logs_created ON operation_logs(created_at);

CREATE TABLE IF NOT EXISTS asset_snapshots (
    id           TEXT PRIMARY KEY,
    asset_id     TEXT NOT NULL,
    record_id    TEXT NOT NULL,
    status       TEXT NOT NULL DEFAULT '',
    status_level TEXT NOT NULL DEFAULT 'unknown',
    summary      TEXT NOT NULL DEFAULT '',
    inspector    TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_asset_snap_asset ON asset_snapshots(asset_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_asset_snap_uniq ON asset_snapshots(asset_id, record_id);

CREATE TABLE IF NOT EXISTS field_observations (
    id           TEXT PRIMARY KEY,
    asset_id     TEXT NOT NULL,
    record_id    TEXT NOT NULL,
    field_key    TEXT NOT NULL,
    field_label  TEXT NOT NULL DEFAULT '',
    value_text   TEXT NOT NULL DEFAULT '',
    value_number REAL,
    source       TEXT NOT NULL DEFAULT '',
    confidence   REAL NOT NULL DEFAULT 0,
    created_at   TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_field_obs_asset_field ON field_observations(asset_id, field_key, created_at);
CREATE UNIQUE INDEX IF NOT EXISTS idx_field_obs_uniq ON field_observations(asset_id, record_id, field_key);

CREATE TABLE IF NOT EXISTS field_confirm_logs (
    id             TEXT PRIMARY KEY,
    record_id      TEXT NOT NULL,
    field_key      TEXT NOT NULL,
    field_label    TEXT NOT NULL DEFAULT '',
    ai_value       TEXT NOT NULL DEFAULT '',
    original_value TEXT NOT NULL DEFAULT '',
    final_value    TEXT NOT NULL DEFAULT '',
    ai_confidence  REAL NOT NULL DEFAULT 0,
    action         TEXT NOT NULL DEFAULT '',
    operator       TEXT NOT NULL DEFAULT '',
    duration_ms    INTEGER NOT NULL DEFAULT 0,
    viewed_photo   INTEGER NOT NULL DEFAULT 0,
    created_at     TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_fcl_record ON field_confirm_logs(record_id, created_at);
