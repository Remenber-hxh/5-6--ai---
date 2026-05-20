-- MySQL 5.7+ / 8.x 兼容。字符集统一 utf8mb4，存储引擎 InnoDB。
-- 时间字段用 VARCHAR(40) 存 RFC3339Nano 字符串（与 SQLite 一致），避免 Go 端跨方言时间解析差异。
-- 大文本字段用 MEDIUMTEXT（16MB 上限，远超巡检记录实际体积）。

CREATE TABLE IF NOT EXISTS records (
    id                  VARCHAR(64)  NOT NULL PRIMARY KEY,
    project             VARCHAR(128) NOT NULL,
    point_id            VARCHAR(64)  NOT NULL,
    point_name          VARCHAR(128) NOT NULL,
    template_id         VARCHAR(64)  NOT NULL,
    template_name       VARCHAR(128) NOT NULL,
    type                VARCHAR(64)  NOT NULL,
    inspector           VARCHAR(64)  NOT NULL,
    capture_attempts    INT          NOT NULL DEFAULT 0,
    manual_required     TINYINT(1)   NOT NULL DEFAULT 0,
    recognition_status  VARCHAR(32)  NOT NULL DEFAULT 'not_started',
    retake_reason       VARCHAR(512) NOT NULL DEFAULT '',
    task_id             VARCHAR(64)  NOT NULL DEFAULT '',
    fields_json         MEDIUMTEXT   NOT NULL,
    images_json         MEDIUMTEXT   NOT NULL,
    report              MEDIUMTEXT   NOT NULL,
    ai_summary          MEDIUMTEXT   NOT NULL,
    ai_summary_tags     TEXT         NOT NULL,
    ai_recommendations  MEDIUMTEXT   NOT NULL,
    ai_summary_error    VARCHAR(512) NOT NULL DEFAULT '',
    submitted           TINYINT(1)   NOT NULL DEFAULT 0,
    submitted_at        VARCHAR(40)  NULL,
    created_at          VARCHAR(40)  NOT NULL,
    updated_at          VARCHAR(40)  NOT NULL,
    INDEX idx_records_created_at (created_at),
    INDEX idx_records_template_id (template_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS assets (
    id                  VARCHAR(255) NOT NULL PRIMARY KEY,
    project_code        VARCHAR(64)  NOT NULL DEFAULT '',
    project             VARCHAR(128) NOT NULL,
    point_id            VARCHAR(64)  NOT NULL DEFAULT '',
    template_id         VARCHAR(64)  NOT NULL DEFAULT '',
    asset_type          VARCHAR(64)  NOT NULL,
    asset_key           VARCHAR(128) NOT NULL DEFAULT '',
    asset_name          VARCHAR(255) NOT NULL,
    last_record_id      VARCHAR(64)  NULL,
    last_status         VARCHAR(32)  NOT NULL DEFAULT '未巡检',
    status_level        VARCHAR(32)  NOT NULL DEFAULT 'unknown',
    status_order        INT          NOT NULL DEFAULT 99,
    last_summary        MEDIUMTEXT   NOT NULL,
    last_inspected_at   VARCHAR(40)  NULL,
    last_inspector      VARCHAR(64)  NOT NULL DEFAULT '',
    last_photo_path     VARCHAR(512) NOT NULL DEFAULT '',
    inspection_count    INT          NOT NULL DEFAULT 0,
    created_at          VARCHAR(40)  NOT NULL,
    updated_at          VARCHAR(40)  NOT NULL,
    INDEX idx_assets_project (project, asset_type),
    INDEX idx_assets_status (last_status, status_level),
    INDEX idx_assets_project_status (project, last_status),
    INDEX idx_assets_project_code (project_code, status_order),
    INDEX idx_assets_asset_key (asset_key),
    INDEX idx_assets_updated_at (updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS ai_tasks (
    id                  VARCHAR(64)  NOT NULL PRIMARY KEY,
    record_id           VARCHAR(64)  NOT NULL,
    status              VARCHAR(32)  NOT NULL DEFAULT 'queued',
    progress_done       INT          NOT NULL DEFAULT 0,
    progress_total      INT          NOT NULL DEFAULT 0,
    error_code          VARCHAR(64)  NOT NULL DEFAULT '',
    error_message       VARCHAR(512) NOT NULL DEFAULT '',
    analysis_json       MEDIUMTEXT   NOT NULL,
    created_at          VARCHAR(40)  NOT NULL,
    updated_at          VARCHAR(40)  NOT NULL,
    INDEX idx_tasks_record_id (record_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS change_requests (
    id              VARCHAR(64)   NOT NULL PRIMARY KEY,
    target_type     VARCHAR(32)   NOT NULL,
    target_id       VARCHAR(255)  NOT NULL,
    patch_json      MEDIUMTEXT    NOT NULL,
    reason          VARCHAR(512)  NOT NULL DEFAULT '',
    status          VARCHAR(32)   NOT NULL DEFAULT 'pending',
    requested_by    VARCHAR(64)   NOT NULL DEFAULT '',
    requested_at    VARCHAR(40)   NOT NULL,
    reviewed_by     VARCHAR(64)   NULL,
    reviewed_at     VARCHAR(40)   NULL,
    review_note     VARCHAR(512)  NOT NULL DEFAULT '',
    applied_at      VARCHAR(40)   NULL,
    INDEX idx_cr_status (status, requested_at),
    INDEX idx_cr_target (target_type, target_id),
    INDEX idx_cr_requester (requested_by, requested_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS submission_idempotency (
    record_id   VARCHAR(64) NOT NULL PRIMARY KEY,
    idem_key    VARCHAR(128) NOT NULL,
    status      VARCHAR(32) NOT NULL DEFAULT 'processing',
    created_at  VARCHAR(40) NOT NULL,
    updated_at  VARCHAR(40) NOT NULL,
    INDEX idx_submission_idem_key (idem_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS roles (
    id          VARCHAR(64)  NOT NULL PRIMARY KEY,
    code        VARCHAR(32)  NOT NULL UNIQUE,
    name        VARCHAR(64)  NOT NULL,
    description VARCHAR(255) NOT NULL DEFAULT '',
    created_at  VARCHAR(40)  NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS departments (
    id         VARCHAR(64)  NOT NULL PRIMARY KEY,
    name       VARCHAR(128) NOT NULL,
    parent_id  VARCHAR(64)  NULL,
    created_at VARCHAR(40)  NOT NULL,
    INDEX idx_departments_parent (parent_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS users (
    id              VARCHAR(64)  NOT NULL PRIMARY KEY,
    username        VARCHAR(64)  NOT NULL UNIQUE,
    display_name    VARCHAR(64)  NOT NULL,
    phone           VARCHAR(32)  NOT NULL DEFAULT '',
    avatar          VARCHAR(512) NOT NULL DEFAULT '',
    role_id         VARCHAR(64)  NOT NULL,
    department_id   VARCHAR(64)  NULL,
    wework_user_id  VARCHAR(128) NOT NULL DEFAULT '',
    password_hash   VARCHAR(255) NOT NULL,
    status          VARCHAR(32)  NOT NULL DEFAULT 'active',
    last_login_at   VARCHAR(40)  NULL,
    created_at      VARCHAR(40)  NOT NULL,
    updated_at      VARCHAR(40)  NOT NULL,
    INDEX idx_users_role (role_id),
    INDEX idx_users_department (department_id),
    INDEX idx_users_wework (wework_user_id),
    INDEX idx_users_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS login_sessions (
    id          VARCHAR(64)  NOT NULL PRIMARY KEY,
    user_id     VARCHAR(64)  NOT NULL,
    token_hash  VARCHAR(128) NOT NULL UNIQUE,
    expire_at   VARCHAR(40)  NOT NULL,
    created_at  VARCHAR(40)  NOT NULL,
    updated_at  VARCHAR(40)  NOT NULL,
    INDEX idx_sessions_user (user_id),
    INDEX idx_sessions_expire (expire_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS operation_logs (
    id          VARCHAR(64)  NOT NULL PRIMARY KEY,
    user_id     VARCHAR(64)  NULL,
    actor_name  VARCHAR(64)  NOT NULL DEFAULT '',
    action      VARCHAR(64)  NOT NULL,
    target_type VARCHAR(64)  NOT NULL DEFAULT '',
    target_id   VARCHAR(255) NOT NULL DEFAULT '',
    detail_json MEDIUMTEXT   NOT NULL,
    created_at  VARCHAR(40)  NOT NULL,
    INDEX idx_operation_logs_user (user_id, created_at),
    INDEX idx_operation_logs_target (target_type, target_id),
    INDEX idx_operation_logs_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
