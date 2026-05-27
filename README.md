# 智巡 InspectAI

> 面向楼宇、园区、物业和设施运维团队的 AI 巡检与资产台账平台。  
> 核心闭环：**拍照取证 → AI 识别 → 人工确认 → 异常复核 → 台账沉淀 → 管理复盘**。

![智巡移动端科技视觉](inspectai/frontend/assets/zhixun-hero-tech.png)

![Go](https://img.shields.io/badge/Backend-Go-00ADD8?style=flat-square)
![Python](https://img.shields.io/badge/AI-Python-3776AB?style=flat-square)
![MySQL](https://img.shields.io/badge/Database-MySQL-4479A1?style=flat-square)
![Docker](https://img.shields.io/badge/Deploy-Docker%20Compose-2496ED?style=flat-square)
![DashScope](https://img.shields.io/badge/Model-Qwen%20VL%20%2B%20Qwen-00A884?style=flat-square)

---

## 项目定位

传统巡检最大的问题不是“能不能填表”，而是现场证据、表单字段、异常判断和后续台账之间长期割裂。巡检员要拍照片、对检查项、抄读数、写日报；主管还要再从日报里翻异常、追问题、做周报。智巡把这一条链路压缩到一个系统里：一线只负责拍照和确认，AI 负责识别、预填、提示风险，后台负责审批、台账和复盘。

当前演示主线聚焦 **资产台账中的电梯设备巡检**：新手巡检员按设备拍照，AI 自动匹配电梯巡检模板，识别现场状态，提示漏拍或风险项，确认后沉淀为巡检记录，并关联到资产台账，主管可以按设备查看历史、导出记录、复核异常。

---

## 界面预览

| 移动端入口 | 管理端登录 | 真实巡检样例 |
|---|---|---|
| ![移动端科技首页](inspectai/frontend/assets/zhixun-hero-tech.png) | ![管理后台登录背景](inspectai/admin-frontend/login-bg-ai.png) | ![水表样例](inspectai/frontend/assets/demo-water-meter.jpg) |

| 电表识别样例 | 配电面板样例 | 异常/漏拍补充样例 |
|---|---|---|
| ![电表样例](inspectai/frontend/assets/demo-electric-meter.jpg) | ![配电面板样例 1](inspectai/frontend/assets/demo-panel-01.jpg) | ![配电面板样例 2](inspectai/frontend/assets/demo-panel-02.jpg) |

---

## 业务闭环

```mermaid
flowchart LR
    A["巡检员进入企业微信/移动端"] --> B["选择设备或巡检模板"]
    B --> C["拍照或相册上传现场图片"]
    C --> D["Qwen-VL 场景识别与字段提取"]
    D --> E{"识别质量是否足够"}
    E -- "不足" --> F["提示补拍，最多三次"]
    F --> C
    E -- "足够" --> G["自动生成日报字段"]
    G --> H["人工确认或修正字段"]
    H --> I["AI 生成总结与行动建议"]
    I --> J["提交巡检记录"]
    J --> K["沉淀资产台账"]
    K --> L["主管复核、导出、问答分析"]
```

这条流程的关键不是让 AI 直接替人下结论，而是让 AI 先做“识别、提示、预填、对比”，再把最终确认权交给人。这样既能减少一线重复录入，又能保证台账里的数据可追溯、可复核。

---

## 技术架构

```mermaid
flowchart TB
    subgraph Client["用户入口"]
        M["移动端：巡检员拍照、确认、提交"]
        A["管理后台：主管审批、复核、台账、看板"]
    end

    subgraph Gateway["接入层"]
        N["Nginx / HTTPS / 反向代理"]
    end

    subgraph Backend["业务服务层"]
        G["Go Backend\n业务 API / 模板 / 任务 / 台账 / 审批"]
        P["Python AI Service\n图片识别 / 字段提取 / 总结问答"]
    end

    subgraph Model["AI 模型层"]
        QV["Qwen-VL Plus\n视觉识别"]
        QT["Qwen Plus\n总结与问答"]
    end

    subgraph Storage["存储层"]
        DB["MySQL\n生产数据"]
        FS["Storage\n上传图片 / 日志 / 临时分类图"]
    end

    M --> N
    A --> N
    N --> G
    G --> P
    P --> QV
    P --> QT
    G --> DB
    G --> FS
```

### 服务职责

| 模块 | 职责 | 默认端口 |
|---|---|---|
| 移动端 | 巡检员拍照、上传、字段确认、提交日报 | `18080` |
| Go 后端 | API、模板、任务、资产台账、审批、导出 | `18080` |
| 管理后台 | 主管看板、资产台账、异常复核、数据中心 | `18081` |
| AI 微服务 | 调用千问视觉模型、文本模型、返回结构化结果 | `19100` |
| MySQL | 生产数据库，沉淀巡检记录、资产、任务、审批 | `3306` |

---

## AI 工作方式

```mermaid
sequenceDiagram
    participant U as 巡检员
    participant FE as 移动端
    participant GO as Go 后端
    participant AI as Python AI 服务
    participant VL as Qwen-VL
    participant DB as MySQL

    U->>FE: 上传现场图片
    FE->>GO: 提交图片和模板信息
    GO->>AI: 请求场景识别/字段提取
    AI->>VL: 多图视觉识别
    VL-->>AI: 场景、字段、置信度、缺失项
    AI-->>GO: 结构化 JSON
    GO-->>FE: 日报字段预填结果
    U->>FE: 人工确认/修正
    FE->>GO: 提交最终字段
    GO->>AI: 请求总结和行动建议
    AI-->>GO: AI 总结
    GO->>DB: 写入巡检记录并更新资产台账
```

AI 部分当前采用“保守识别”策略：能看清的字段才填，无法确认的字段标记为需人工复核；关键图片缺失时提示补拍，不用编造数据污染台账。对于数字读数类场景，提示词会要求模型保留原始读数、单位、小数点位置和置信度，最终仍由人工确认后入库。

---

## 当前能力

| 能力域 | 已实现内容 |
|---|---|
| 移动端巡检 | 拍照识别、相册上传、手动选模板、字段确认、失败重拍、人工兜底 |
| AI 识别 | 场景分类、表单字段提取、缺失照片提示、AI 总结、行动建议 |
| 巡检管理 | 巡检计划、巡检任务、巡检记录、状态追踪、导出 |
| 资产台账 | 设备档案、历史巡检、最近状态、异常摘要、记录关联 |
| 异常处理 | 异常复核、修改申请、主管审批、操作留痕 |
| 数据看板 | 巡检趋势、资产状态、异常统计、AI 问答辅助 |
| 部署能力 | 本地一键启动、Docker Compose 生产部署、MySQL、Docker Secret、Nginx |

---

## 演示主线

完整演示手稿见 [docs/DEMO.md](docs/DEMO.md)。

建议演示按一条线讲完，避免来回跳页面：

1. 移动端进入巡检，选择“无机房电梯”模板。
2. 上传电梯现场照片，模拟新手漏拍按钮面板。
3. AI 自动识别为电梯巡检，预填检查字段。
4. 系统提示缺失关键照片或风险项，巡检员提交修改申请。
5. 管理后台收到审批，主管处理后进入台账。
6. 在资产台账筛选“无机房电梯”，查看该设备历史巡检。
7. 导出巡检记录，说明主管不再逐条翻日报，而是看 AI 摘要和趋势变化。

---

## 快速启动

### 本地开发

```powershell
cd inspectai
.\scripts\setup-key.ps1
.\scripts\start-local.ps1
.\scripts\start-admin.ps1
```

访问入口：

| 入口 | 地址 |
|---|---|
| 移动端 / Go 后端 | <http://127.0.0.1:18080> |
| 管理后台 | <http://127.0.0.1:18081> |
| AI 健康检查 | <http://127.0.0.1:19100/health> |

健康检查：

```powershell
curl --noproxy "*" http://127.0.0.1:18080/health
curl --noproxy "*" http://127.0.0.1:19100/health
```

停止服务：

```powershell
.\scripts\stop-admin.ps1
.\scripts\stop-local.ps1
```

### 服务器部署

服务器部署文档见 [inspectai/docs/DEPLOY.md](inspectai/docs/DEPLOY.md)。

```bash
git clone https://github.com/Remenber-hxh/5-6--ai---.git
cd 5-6--ai---/inspectai

cp .env.prod.example .env.prod
# 编辑 .env.prod，只写非敏感配置

export DASHSCOPE_API_KEY="sk-xxx"
export MYSQL_PASSWORD="change-me"
export MYSQL_ROOT_PASSWORD="change-me"
export INSPECTAI_ADMIN_PASSWORD="change-me"

bash scripts/prepare-secrets.sh
bash scripts/deploy-linux.sh
```

生产部署约束：

| 项目 | 要求 |
|---|---|
| 域名 | 企业微信可信域名需要指向服务器 |
| HTTPS | Nginx 侧配置证书，企业微信内访问必须 HTTPS |
| 密钥 | 千问 Key、MySQL 密码、管理员密码走 Docker Secret |
| 数据库 | 生产使用 MySQL，不建议用 SQLite |
| 备份 | MySQL 和上传图片目录需要定期备份 |
| 日志 | `storage/logs` 保留启动、AI、后端、管理端日志 |

---

## 目录结构

```text
.
├─ README.md                         项目总览，适合 GitHub 首页展示
├─ docs/
│  └─ DEMO.md                        演示手稿与讲解脚本
└─ inspectai/
   ├─ go-backend/                    Go 业务后端
   ├─ ai-service/                    Python AI 微服务与 prompts
   ├─ frontend/                      移动端页面
   ├─ admin-frontend/                管理后台页面
   ├─ nginx/                         生产反向代理配置
   ├─ scripts/                       启停、密钥、部署、打包脚本
   ├─ storage/                       本地运行数据，已 gitignore
   ├─ docs/
   │  └─ DEPLOY.md                   服务器部署指南
   ├─ docker-compose.yml             本地/开发编排
   └─ docker-compose.prod.yml        生产部署编排
```

---

## 数据沉淀方式

```mermaid
erDiagram
    ASSET ||--o{ INSPECTION_RECORD : has
    INSPECTION_RECORD ||--o{ RECORD_FIELD : contains
    INSPECTION_RECORD ||--o{ RECORD_PHOTO : attaches
    INSPECTION_RECORD ||--o{ REVIEW_REQUEST : may_create
    USER ||--o{ INSPECTION_RECORD : submits
    USER ||--o{ REVIEW_REQUEST : approves

    ASSET {
        string asset_no
        string asset_name
        string asset_type
        string location
        string status
    }

    INSPECTION_RECORD {
        string record_id
        string template_code
        string project_name
        string result_status
        datetime submitted_at
        string ai_summary
    }

    RECORD_FIELD {
        string field_key
        string field_label
        string field_value
        string source
        float confidence
    }

    RECORD_PHOTO {
        string photo_url
        string photo_type
        string ai_note
    }

    REVIEW_REQUEST {
        string reason
        string status
        string reviewer
        datetime reviewed_at
    }
```

资产台账保存的是“设备长期档案”，巡检记录保存的是“某一次巡检事实”。两者分开后，主管既能看单次问题，也能看同一设备长期趋势。

---

## 路线图

### 近期

- 企业微信 OAuth / SSO 接入，替代轻量令牌登录。
- 审批通过后的状态联动继续增强，让异常闭环更直观。
- AI 行动建议在移动端和管理端进一步突出。
- 数据导出模板标准化，贴近日常汇报口径。

### 中期

- 多租户与项目级权限，支持多个园区或物业项目共用。
- 设备健康评分、趋势预警、周度/月度自动摘要。
- 对象存储接入，图片从本地磁盘迁移到 OSS/S3。
- CI/CD 与镜像仓库，形成可回滚的版本发布流程。

### 长期

- 接入智能穿戴设备，支持语音、视频、定位和现场传感数据。
- 接入 MQTT / Modbus / IoT 网关，实现 AI 巡检与设备实时数据互证。
- 和 BIM / 数字孪生结合，把巡检记录挂到具体楼层、房间、设备。

---

## 项目边界

当前版本适合演示、试点和小范围内部验证；如果要正式生产上线，建议优先补齐：

- 企业微信身份体系和细粒度权限。
- 数据库备份、日志留存、异常告警。
- 更完整的测试覆盖和 CI/CD。
- AI 识别结果的人工复核规范。
- 图片长期存储、脱敏和访问权限控制。

---

## 维护者

- 项目仓库：[Remenber-hxh/5-6--ai---](https://github.com/Remenber-hxh/5-6--ai---)
- 项目名称：智巡 InspectAI
- 当前 License：未声明，默认不开放商用复用授权。
