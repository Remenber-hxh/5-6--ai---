# InspectAI Assistant

AI 巡检填报助手 — 把 "拍照 → AI 识别字段 → 人工确认 → 提交日报 → 沉淀资产台账" 串成一条移动端可跑的闭环。

> 当前实现状态、目录详细注释和与 codex 第一版的差异，看 [`docs/plan/16-Claude-当前架构与目录总览.md`](../docs/plan/16-Claude-当前架构与目录总览.md)。本 README 只讲怎么跑起来。

## 一键启动

```powershell
cd D:\5-6月 ai 大会\inspectai
.\scripts\start-local.ps1
```

启动脚本会一次拉起两个本地进程：

- Go 后端 + 前端入口：<http://127.0.0.1:18080>
- Python AI 微服务：<http://127.0.0.1:19100>

健康检查：

```powershell
curl --noproxy "*" http://127.0.0.1:18080/health
curl --noproxy "*" http://127.0.0.1:19100/health
```

停止：

```powershell
.\scripts\stop-local.ps1
```

## 接入千问 DashScope

密钥**不能**写在 `.env` 明文，用 DPAPI 加密：

```powershell
.\scripts\setup-key.ps1
# 交互式输入 sk-... ，加密落 .env.secure（绑当前 Windows 用户）
# 之后 start-local.ps1 启动时会自动解密注入 DASHSCOPE_API_KEY
```

`.env`（非密钥配置）：

```env
INSPECTAI_AUTH_TOKEN=
INSPECTAI_SUPERVISOR_TOKEN=
CORS_ALLOWED_ORIGINS=
TMP_IMAGE_TTL_HOURS=24
QWEN_VISION_MODEL=qwen-vl-plus
QWEN_TEXT_MODEL=qwen-plus
```

AI 调用失败不回退伪造数据，会返回 `retake_required` 让用户重拍或转人工，避免假数据污染台账。

本地默认 `INSPECTAI_AUTH_TOKEN` 为空，方便演示和调试；生产环境必须设置访问令牌。`INSPECTAI_AUTH_TOKEN` 用于移动端巡检用户，`INSPECTAI_SUPERVISOR_TOKEN` 用于后台审批/复核。设置后，前端首次请求业务接口会提示输入令牌，并由后端下发 HttpOnly cookie 保护后续 `/storage/` 图片访问。

## 当前能力

- ✅ 移动端首屏：拍照 / 从相册上传 / 手动选择模板
- ✅ 已恢复完整模板入口：紫涵雅集能耗抄表/综合巡检 + 会议中心机房/水泵/扶梯/电梯等巡检
- ✅ 能耗抄表恢复 6 项读数：Z1/Z2/Z3/Z4、生活水表、消防水表
- ✅ qwen-vl-plus 场景识别 + 已接入模板字段识别（约 3-10s）
- ✅ qwen-plus AI 总结 + 行动建议（约 5-10s）
- ✅ 字段级人工确认 / 编辑，AI 不覆盖人工修改
- ✅ 失败重拍 3 次后自动转人工填写
- ✅ MySQL / SQLite 双存储：本地可接 MySQL，开发兜底可用 SQLite
- ✅ 提交日报与资产台账写入已做事务化，避免日报成功但台账丢失
- ✅ 资产台账自动沉淀，支持详情、历史、修改申请和审批流转
- ✅ 临时识别图片按 `TMP_IMAGE_TTL_HOURS` 自动清理
- ✅ 提交时后端校验必填字段 + needsReview

## 当前边界

- 访问控制目前是轻量令牌模式，不是企业微信 OAuth / SSO；正式上线前建议接入企业微信身份体系
- 会议中心部分模板目前恢复为人工填写路径，未接成熟视觉 prompt 的场景不会强行 AI 识别
- 台账修改采用“申请-审批”方式，适合演示和轻量管理；如果要做生产审计，需要补充更完整的操作日志和权限模型
- MySQL 表结构已能支撑 records / assets / ai_tasks，但外键约束、归档策略、备份恢复策略还需要按生产标准继续完善
- 云端部署见 [`DEPLOY.md`](DEPLOY.md)，企业微信可信域名、HTTPS 证书、反向代理仍需要在服务器侧配置

## 目录速览

```
inspectai/
├─ ai-service/         Python AI 微服务 + prompts/
├─ frontend/           HTML+CSS+JS（无构建）
├─ go-backend/         Go 1.24 单二进制
├─ scripts/            PowerShell 启停 + setup-key
├─ storage/            uploads/ + tmp_classify/ + logs/ + 本地数据库文件
├─ nginx/              Nginx 反代示例（未启用）
├─ docker-compose.yml  容器编排示例（未启用）
├─ .env                非密钥配置
└─ .env.secure         DPAPI 加密的 DashScope key（gitignore）
```

详细每层职责、数据流、调试命令，见 [`docs/plan/16-Claude-当前架构与目录总览.md`](../docs/plan/16-Claude-当前架构与目录总览.md)。
