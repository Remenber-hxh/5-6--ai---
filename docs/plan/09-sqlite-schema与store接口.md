# SQLite Schema + Store 接口设计

> 解决 `08-` 中 M2（数据无持久化）。
> 给 codex 直接照着实现，不需要再设计。
> 用 `modernc.org/sqlite`（纯 Go，无 cgo 依赖，Go 1.24 + Windows 完全 ok）。

## 0. 选型决定

| 维度 | 决定 |
| --- | --- |
| 库 | `modernc.org/sqlite` ([GitHub](https://gitlab.com/cznic/sqlite)) |
| 驱动名 | `sqlite`（注意不是 `sqlite3`） |
| 文件位置 | `${STORAGE_DIR}/inspectai.db`（默认 `storage/inspectai.db`） |
| 迁移策略 | 启动时跑一次 `CREATE TABLE IF NOT EXISTS ...`，手写不引入额外 migration 库 |
| 并发 | sqlite 写串行，配 `?_journal_mode=WAL&_busy_timeout=5000` |
| JSON 字段 | 用 SQLite 原生 JSON1（`fields TEXT` 存 JSON 字符串，查询时用 `json_extract`） |

依赖添加：
```bash
cd inspectai/go-backend
go get modernc.org/sqlite
go mod tidy
```

## 1. 三张表 DDL

放到 `inspectai/go-backend/cmd/server/schema.sql`（或直接嵌入 Go 字符串常量）：

```sql
-- 巡检记录主表
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
    manual_required     INTEGER NOT NULL DEFAULT 0,  -- 0/1
    recognition_status  TEXT NOT NULL DEFAULT 'not_started',
    retake_reason       TEXT NOT NULL DEFAULT '',
    task_id             TEXT NOT NULL DEFAULT '',
    fields_json         TEXT NOT NULL DEFAULT '[]',  -- []FieldValue 序列化
    check_items_json    TEXT NOT NULL DEFAULT '[]',
    report              TEXT NOT NULL DEFAULT '',
    ai_summary          TEXT NOT NULL DEFAULT '',
    ai_summary_tags     TEXT NOT NULL DEFAULT '[]',  -- []string 序列化
    submitted           INTEGER NOT NULL DEFAULT 0,
    submitted_at        TEXT,                         -- RFC3339, nullable
    created_at          TEXT NOT NULL,                -- RFC3339
    updated_at          TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_records_created_at ON records(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_records_template_id ON records(template_id);
CREATE INDEX IF NOT EXISTS idx_records_submitted   ON records(submitted, created_at DESC);

-- 上传图片表
CREATE TABLE IF NOT EXISTS images (
    id              TEXT PRIMARY KEY,
    record_id       TEXT NOT NULL REFERENCES records(id) ON DELETE CASCADE,
    file_name       TEXT NOT NULL,
    path            TEXT NOT NULL,                    -- 本地绝对路径
    image_type      TEXT NOT NULL DEFAULT 'unknown',
    size_bytes      INTEGER NOT NULL,
    content_hash    TEXT NOT NULL,
    created_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_images_record_id ON images(record_id, created_at);

-- 资产台账表
CREATE TABLE IF NOT EXISTS assets (
    id              TEXT PRIMARY KEY,                 -- {project}_{template_id}_{asset_no} 拼接
    project         TEXT NOT NULL,
    asset_type      TEXT NOT NULL,                    -- 来自 ReportTemplate.AssetType
    asset_name      TEXT NOT NULL,                    -- 显示名（资产编号或点位名）
    last_record_id  TEXT REFERENCES records(id),
    last_status     TEXT NOT NULL DEFAULT '未巡检',    -- 正常 / 异常 / 待复核
    last_summary    TEXT NOT NULL DEFAULT '',
    inspection_count INTEGER NOT NULL DEFAULT 0,      -- 累计巡检次数
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_assets_project ON assets(project, asset_type);
CREATE INDEX IF NOT EXISTS idx_assets_status  ON assets(last_status);

-- AI 分析任务表（轻量化，主要存中间状态）
CREATE TABLE IF NOT EXISTS ai_tasks (
    id              TEXT PRIMARY KEY,
    record_id       TEXT NOT NULL REFERENCES records(id) ON DELETE CASCADE,
    status          TEXT NOT NULL DEFAULT 'queued',
    progress_done   INTEGER NOT NULL DEFAULT 0,
    progress_total  INTEGER NOT NULL DEFAULT 0,
    error_code      TEXT NOT NULL DEFAULT '',
    error_message   TEXT NOT NULL DEFAULT '',
    analysis_json   TEXT NOT NULL DEFAULT '{}',       -- AIAnalysis 序列化
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_tasks_record_id ON ai_tasks(record_id, created_at DESC);
```

## 2. Go 端接口设计

把当前 `Store struct` 抽成接口，便于内存版 → SQLite 版无痛切换：

```go
// store.go

type Store interface {
    // === 点位 / 模板（静态，从内存返回）===
    Points() []Point
    PointByID(id string) (Point, bool)
    Templates() []ReportTemplate
    TemplateByID(id string) (ReportTemplate, bool)

    // === 记录 ===
    CreateRecord(rec *Record) error
    GetRecord(id string) (*Record, error)        // 返回深拷贝
    UpdateRecord(rec *Record) error              // 全量替换
    ListRecords(limit int) ([]*Record, error)

    // === 图片 ===
    AppendImage(recordID string, img ImageInfo) error
    ListImages(recordID string) ([]ImageInfo, error)

    // === AI 任务 ===
    CreateTask(task *AITask) error
    GetTask(id string) (*AITask, error)
    UpdateTask(id string, mutate func(*AITask)) error
    LatestTaskByRecord(recordID string) (*AITask, error)

    // === 资产台账 ===
    UpsertAsset(asset *AssetEntry) error
    ListAssets() ([]*AssetEntry, error)
    GetAsset(id string) (*AssetEntry, error)

    // === 生命周期 ===
    Close() error
}

// 两份实现：
type MemStore struct { /* 当前的内存实现 */ }
type SQLiteStore struct {
    db *sql.DB
}
```

handler 层不依赖具体实现，只依赖接口；启动时在 `main()` 选：

```go
func main() {
    var store Store
    if dbPath := os.Getenv("SQLITE_PATH"); dbPath != "" {
        s, err := NewSQLiteStore(dbPath)
        if err != nil {
            log.Fatalf("sqlite: %v", err)
        }
        store = s
    } else {
        store = NewMemStore()  // 测试 / 临时演示
    }
    defer store.Close()
    server := &Server{store: store, ...}
    ...
}
```

## 3. 关键操作的 SQL 范例

### 创建记录

```go
func (s *SQLiteStore) CreateRecord(rec *Record) error {
    fields, _ := json.Marshal(rec.Fields)
    items, _ := json.Marshal(rec.CheckItems)
    tags, _ := json.Marshal([]string{})
    _, err := s.db.Exec(`
        INSERT INTO records (
            id, project, point_id, point_name, template_id, template_name,
            type, inspector, capture_attempts, manual_required,
            recognition_status, retake_reason, task_id,
            fields_json, check_items_json, report, ai_summary, ai_summary_tags,
            submitted, submitted_at, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        rec.ID, rec.Project, rec.PointID, rec.PointName, rec.TemplateID, rec.TemplateName,
        rec.Type, rec.Inspector, rec.CaptureAttempts, btoi(rec.ManualRequired),
        rec.RecognitionStatus, rec.RetakeReason, rec.TaskID,
        string(fields), string(items), rec.Report, rec.AISummary, string(tags),
        btoi(rec.Submitted), nullableTime(rec.SubmittedAt),
        rec.CreatedAt.Format(time.RFC3339Nano), time.Now().Format(time.RFC3339Nano),
    )
    return err
}
```

### 全量更新（最简单可靠的并发策略）

```go
func (s *SQLiteStore) UpdateRecord(rec *Record) error {
    fields, _ := json.Marshal(rec.Fields)
    items, _ := json.Marshal(rec.CheckItems)
    _, err := s.db.Exec(`
        UPDATE records SET
            project=?, point_id=?, point_name=?, template_id=?, template_name=?,
            type=?, inspector=?, capture_attempts=?, manual_required=?,
            recognition_status=?, retake_reason=?, task_id=?,
            fields_json=?, check_items_json=?, report=?, ai_summary=?,
            submitted=?, submitted_at=?, updated_at=?
        WHERE id=?`,
        rec.Project, ..., string(fields), string(items), ...,
        time.Now().Format(time.RFC3339Nano),
        rec.ID,
    )
    return err
}
```

### 列出记录

```go
func (s *SQLiteStore) ListRecords(limit int) ([]*Record, error) {
    if limit <= 0 { limit = 100 }
    rows, err := s.db.Query(`
        SELECT id, project, point_id, point_name, template_id, template_name,
               type, inspector, capture_attempts, manual_required,
               recognition_status, retake_reason, task_id,
               fields_json, check_items_json, report, ai_summary,
               submitted, submitted_at, created_at
        FROM records
        ORDER BY created_at DESC
        LIMIT ?`, limit)
    if err != nil { return nil, err }
    defer rows.Close()
    var out []*Record
    for rows.Next() {
        rec := &Record{}
        var fieldsJSON, itemsJSON string
        var submittedAt sql.NullString
        if err := rows.Scan(&rec.ID, ..., &fieldsJSON, &itemsJSON, ..., &submittedAt, &rec.CreatedAt); err != nil {
            return nil, err
        }
        json.Unmarshal([]byte(fieldsJSON), &rec.Fields)
        json.Unmarshal([]byte(itemsJSON), &rec.CheckItems)
        if submittedAt.Valid {
            t, _ := time.Parse(time.RFC3339Nano, submittedAt.String)
            rec.SubmittedAt = &t
        }
        out = append(out, rec)
    }
    return out, rows.Err()
}
```

### Asset Upsert

```go
func (s *SQLiteStore) UpsertAsset(asset *AssetEntry) error {
    now := time.Now().Format(time.RFC3339Nano)
    _, err := s.db.Exec(`
        INSERT INTO assets (id, project, asset_type, asset_name, last_record_id,
                            last_status, last_summary, inspection_count, created_at, updated_at)
        VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            last_record_id   = excluded.last_record_id,
            last_status      = excluded.last_status,
            last_summary     = excluded.last_summary,
            asset_name       = excluded.asset_name,
            inspection_count = inspection_count + 1,
            updated_at       = excluded.updated_at
    `, asset.ID, asset.Project, asset.AssetType, asset.AssetName, asset.LastRecordID,
        asset.Status, asset.Summary, now, now)
    return err
}
```

## 4. 启动 / 迁移

```go
//go:embed schema.sql
var schemaSQL string

func NewSQLiteStore(path string) (*SQLiteStore, error) {
    dsn := path + "?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on"
    db, err := sql.Open("sqlite", dsn)
    if err != nil { return nil, err }
    db.SetMaxOpenConns(1) // sqlite 写串行，简单粗暴，避免 busy
    if _, err := db.Exec(schemaSQL); err != nil {
        return nil, fmt.Errorf("migrate: %w", err)
    }
    return &SQLiteStore{db: db}, nil
}
```

## 5. .env 增加

```env
SQLITE_PATH=./storage/inspectai.db
```

启动脚本 `start-local.ps1` 同步加默认值：
```powershell
Set-DefaultEnv "SQLITE_PATH" "./storage/inspectai.db"
```

## 6. 资产 ID 拼接规则（修 `08-` O3）

```go
func assetID(rec *Record, assetNo string) string {
    if assetNo == "" {
        assetNo = rec.PointID  // 用 PointID 而非 PointName，避免重命名后撞
    }
    return fmt.Sprintf("%s::%s::%s", rec.Project, rec.TemplateID, assetNo)
}
```

`::` 作为分隔符（汉字、空格、中划线都不会出现），保证 key 唯一且可读。

## 7. 测试套（codex 实现完后跑一遍）

最小冒烟，验证落库不丢：

```text
1. 启动 → 检查 storage/inspectai.db 自动创建
2. 创建记录 → 立刻 GET → 字段一致
3. 上传 3 张图 → 列出 → 路径正确
4. 触发 AI → 任务记录入库
5. 改字段 → 重启服务 → 字段还在 ★
6. 提交 → 资产入库 → 列资产能看到
7. 第 2 次提交同资产 → inspection_count 变 2
```

第 5 步是 SQLite 的核心价值，**必测**。

## 8. 排期

如果 codex 现在开工：
- DDL + Schema 嵌入：30 分钟
- Store 接口抽离：30 分钟
- SQLiteStore 实现：1.5 小时（核心 CRUD + JSON 序列化）
- 替换 main.go 中的 Store 字段类型：30 分钟
- 测 1-7 步冒烟：30 分钟
- 总计：**3-3.5 小时**

5/12 上午就能搞定，不挤后面的 prompt 联调时间。

## 9. 不做（明确边界）

- ❌ ORM（gorm 等）：第一版三张表手写 SQL 更可控
- ❌ 数据库迁移工具（goose / golang-migrate）：手写 IF NOT EXISTS 够用
- ❌ 触发器 / 视图：所有派生数据在 Go 层算
- ❌ 全文索引 / FTS5：第一版搜索用 LIKE 即可，不到瓶颈

## 10. 升级 PostgreSQL 路径

未来要切 PG：
- 接口已抽出，只需新增 `PgStore`
- DDL 几乎兼容（去掉 `INTEGER 0/1`，改 `BOOLEAN`；时间类型改 `TIMESTAMPTZ`）
- JSON 字段改 `JSONB`，`json_extract` 改 `->>` 操作符
- 总迁移成本约半天
