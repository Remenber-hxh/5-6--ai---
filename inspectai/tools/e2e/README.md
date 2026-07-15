# e2e 自查工具

发版前本地冒烟用,不进 CI(依赖本地跑着的服务与 Chrome)。

## 前置

1. 后端 + AI 服务:管理员 PowerShell 跑 `scripts/start-local.ps1`
2. 新版后台 dev:`cd admin-web && npm run dev`(18090)
3. `pip install playwright && playwright install chrome`(一次性)

## 脚本

| 脚本 | 用途 |
|---|---|
| `shot.py [route] [out.png] [wait_ms]` | 登录后截任意路由,肉眼核对 UI |
| `smoke_asset_crud.py` | 资产台账 新增→删除 全链路冒烟,退出码 0=过 |

统一环境变量:`E2E_BASE`(默认 :18090)、`E2E_API`(默认 :18080)、`E2E_USER`、`E2E_PASS`。

也可从仓库根目录 `make verify` 触发冒烟。
