# 智巡 InspectAI

面向物业 / 设施管理的 **AI 巡检系统**:现场拍照即生成日报,异常自动进入整改闭环,全程留痕可追溯。

对外产品名 **智巡**,内部代号 `inspectai`。线上:<https://jadeast.cloud>

---

## 它解决什么

传统巡检的三个痛点:

| 痛点 | 智巡的做法 |
|---|---|
| 回办公室补填表格,耗时且失真 | 现场拍照 → AI 识别读数与状态 → 逐项确认即提交 |
| 发现异常后跟不下去 | 异常自动生成待整改任务 → 复检 → 销账,闭环可查 |
| 台账靠人工汇总,对不上 | 每次巡检自动沉淀资产快照与字段观测,趋势可回溯 |

**定位是"辅助填报 + 全程留痕",不是替代人。** AI 只出建议,每个字段都要巡检员确认才入库;
谁在什么时间改了什么,操作日志全留。签字确认权始终在人。

---

## 系统构成

```
                    ┌──────────────┐
   移动端(巡检员)  │              │   管理后台(主管/管理员)
   frontend/  ─────▶│  go-backend  │◀───── admin-web/
   mobile-web/(新) │   :18080     │       (React + antd)
                    │              │
                    └──────┬───────┘
                           │
                    ┌──────▼───────┐        ┌─────────┐
                    │  ai-service  │───────▶│ 阿里云百炼 │ 视觉识别
                    │    :19100    │        │ DeepSeek  │ 文本分析
                    └──────────────┘        └─────────┘
                           │
                    ┌──────▼───────┐
                    │    MySQL     │  巡检记录 / 资产台账 / 审批 / 日志
                    └──────────────┘
```

### 各服务职责

| 服务 | 技术 | 职责 |
|---|---|---|
| `go-backend` | Go,单二进制 | 业务主干:记录、资产、审批、工程任务、权限、租户。同时托管旧移动端静态页 |
| `ai-service` | Python,仅标准库 | AI 调用层:视觉识别、文本总结、管理问答。屏蔽厂商差异 |
| `admin-web` | React 18 + Vite + TS + antd | 新管理后台,线上挂在 `/v2/`。**与旧后台 `/admin/` 并存**,尚未接管主路由 |
| `mobile-web` | React 18 + Vite + TS + Arco Design Mobile | **新移动端,功能已对齐旧版,本地验收中**(未上生产),与旧版并存 |
| `frontend` | 原生 JS SPA | 旧移动端(生产在用),待新版达标后退役 |
| `admin-frontend` | 原生 JS | 旧后台,**线上仍是主路由** `/admin/`(见 `nginx/nginx.conf`),未退役 |

---

## 架构要点

### 数据访问:Store 接口 + 双实现

`Store` 按业务域拆成 10 余个接口(`RecordStore` / `AssetStore` / `TenantStore` …),
组合成全量 `Store`。两个实现:

- `SQLiteStore` —— 同时支持 SQLite 与 MySQL(方言分支),生产用 MySQL
- `MemStore` —— 内存实现,供测试与降级

编译期断言保证两者都覆盖全部域,漏实现直接编译失败。

### 版本化数据库迁移

`migrations.go` 一张表管全部 schema 变更,`schema_migrations` 记账,只往后加、不改历史条目。
当前已到 **010**。新增迁移须幂等,双方言差异走 `s.dialect` 分支。

### 路由权限表

`routes.go` 用一张表声明"哪个接口谁能调"(方法 / 路径 / 守卫 / 能力键)。
守卫分四档:登录即可 / 管理角色 / 系统管理员 / **平台超管**。
**前端隐藏菜单只是体验,后端逐条校验才是边界** —— 绕过前端直接调接口同样被拦。

### 权限矩阵

角色 × 能力可在后台可视化配置,支持自定义角色。内置四角色:
`admin` / `manager` / `supervisor` / `inspector`。

### 多租户(Phase 0 已完成)

一套部署、多客户数据隔离。策略:**共享 schema + `tenant_id` 行级隔离**。

- 租户从**登录账号自动带出**,用户不手选公司
- 隔离**收口在 Store 层**:读写方法签名带租户,漏改的调用点编译不过
- 跨租户访问一律等同"不存在"(404),不泄露存在性
- **两级管理员**:平台超管(唯一可跨租户、建客户) vs 租户管理员(锁死本租户)

> 当前已隔离:记录域、资产域。其余业务域按"临上第二客户前补齐"的节奏推进,
> 未隔离处均有 TODO 标注并说明为何单客户下当前行为正确。

---

## 业务闭环

```
拍照 → AI 识别 → 逐项确认 → 提交日报
                                  │
                          发现异常 ▼
                    自动生成待整改任务 → 复检 → 销账关闭
```

数据修改走审批流:巡检员提申请 → 主管审批 → 通过后应用并同步资产状态、自动销账。

---

## 快速启动

### 本地开发

```bash
# 一键起后端 + AI 服务 + 管理后台
powershell -ExecutionPolicy Bypass -File inspectai/scripts/start-all.ps1
```

| 服务 | 地址 |
|---|---|
| 旧移动端 / 后端 | <http://localhost:18080> |
| 管理后台 admin-web | <http://localhost:18090> |
| 新移动端 mobile-web | <http://localhost:18091>(`cd inspectai/mobile-web && npm run dev`) |
| AI 服务健康检查 | <http://localhost:19100/health> |

前置:本机 MySQL 服务需启动。

### 生产部署

```bash
cd /opt/inspectai-src/inspectai
git pull
bash scripts/backup.sh          # 部署前先备份
bash scripts/deploy-linux.sh    # 构建 + 滚动重启
```

详见 [`inspectai/docs/DEPLOY.md`](inspectai/docs/DEPLOY.md)。

---

## 运维

### 备份与恢复

```bash
bash scripts/backup.sh              # 数据库 + 巡检照片 + 密钥,一条命令
bash scripts/restore.sh --list      # 看有哪些备份
bash scripts/restore.sh <日期>      # 恢复演练(→ 临时库,不碰生产)
```

- `mysqldump --single-transaction` 热备**不锁表**,业务无感
- 备份前查磁盘余量,不足即中止;备份后按保留策略清理(日备 7 天,周日备份 28 天)
- 失败推企业微信群机器人
- **恢复默认到临时库**,覆盖生产须显式 `--force-production` 并手工确认

> 没恢复成功过的备份不算备份 —— 定期跑一次演练。

### 安全

- 密码 PBKDF2-SHA256 12 万次迭代 + 每人独立盐;令牌不明文存库,8 小时过期
- 登录防爆破:同账号+IP 连错 5 次锁 10 分钟
- 密钥经 Docker Secret 文件注入,不落明文配置
- 全站 HTTPS(TLS 1.2/1.3 + HSTS),HTTP 强制跳转
- 全参数化 SQL

---

## 目录结构

```
inspectai/
├── go-backend/cmd/server/   业务主干(~15k 行,31 个测试)
├── ai-service/              AI 调用层
├── admin-web/               新管理后台(React,线上 /v2/)
├── admin-frontend/          旧管理后台(原生 JS,线上 /admin/ —— 仍是主路由)
├── mobile-web/              新移动端(React,本地验收中)
├── frontend/                旧移动端(原生 JS,生产在用)
├── scripts/                 部署 / 备份 / 恢复 / 本地启动
├── nginx/                   反向代理与 TLS 配置
└── docs/                    活文档;archive/ 放已完成或已废弃的方案
```

关键文档:

- [`docs/tenant-and-auth-design.md`](inspectai/docs/tenant-and-auth-design.md) —— 多租户与登录设计
- [`docs/DEPLOY.md`](inspectai/docs/DEPLOY.md) —— 部署、备份、排错
- [`docs/product-notes.md`](inspectai/docs/product-notes.md) —— 产品待解决问题

---

## 路线图

**进行中 —— 移动端重构 Phase 1**
新移动端与旧版并存,逐屏迁移。当前:脚手架与应用外壳已完成;
下一步为拍照托盘 + 离线队列(弱网现场先存照片,联网自动上传补 AI 识别)。

**下一阶段**
- 移动端逐屏迁移至新架构,达标后退役旧版
- 按客户配置:各租户的模板 / 字段 / 品牌
- 其余业务域租户隔离补齐(触发条件:接入第二个客户前)

**技术债(已知,有条件触发)**
- 接口限流、安全响应头、密码复杂度策略
- 自动备份定时任务上线、异地备份副本

---

## 项目边界

- AI 输出仅供参考,**最终数据以人工确认为准**
- 图片识别调用境内厂商服务(阿里云 / DeepSeek),**数据不出境**;
  涉密场景支持私有化模型部署
- 当前为单租户运行(所有数据归默认租户),多客户能力已具备地基但尚未启用

---

*苏ICP备2026048624号*
