# 归档:已完成或已废弃的方案与审计

这里放**做完了的事**和**没被采纳的方案**。它们记录当时的判断依据,有考古价值,
但**不是当前有效的指导** —— 别照着做,也别当成系统现状的描述。

`docs/` 根目录下只留活文档(部署、本地开发、当前架构与设计)。

| 文件 | 是什么 | 现状 |
|---|---|---|
| `COMPETITION_BREAKTHROUGH_PLAN.md` | 比赛突破方案与彩排前优先级(2026-05-28) | 比赛已过 |
| `insights-deepseek-plan.md` | 智能洞察 + DeepSeek 首版方案 | 文件自己标了 SUPERSEDED,被下一条取代 |
| `insights-board-redesign-final.md` | 数据看板重设计 + 管理 AI 合并方案(2026-05-31) | 已实施 |
| `DEEPSEEK_MANAGEMENT_AI_IMPLEMENTATION_PLAN_2026-05-31.md` | 管理后台历史分析与问答实施方案 | 已实施;管理 AI 后来又升级为 L2 一键执行 agent |
| `whole-project-audit-2026-06.md` | 全项目体检(2026-06-01) | 一次性审计,结论已消化 |
| `INSPECTION_PLAN_TASK_CLOSED_LOOP_DESIGN_2026-06-05.md` | 巡检计划与任务闭环设计 | 已实施 |
| `ENGINEERING_PLAN_TASK_CLOSED_LOOP_REAL_PLAN_2026-06-05.md` | 工程计划闭环修正版 | 已实施 |
| `FRONTEND_REFACTOR_RECTIFICATION_PLAN_2026-07-05.md` | 前端整改交接方案(codex 时期) | **技术路线未被采用**,详见文件头;第 7、8 节仍值得读 |

## 当前有效的文档在哪

| 想知道 | 看 |
|---|---|
| 怎么部署、服务器上什么结构 | `../DEPLOY.md` |
| 本地怎么起服务、测试账号 | `../LOCAL-DEV.md` |
| 数据库迁移与 push-db 的红线 | `../DB_MIGRATION_OVERWRITE.md` |
| 多租户与登录是怎么设计的 | `../tenant-and-auth-design.md` |
| 移动端组件库路线 | `../MIGRATE-ARCO-MOBILE.md` |
| 移动端浅色页的设计规矩与色彩令牌 | `../MOBILE-WEB-OPTIMIZATION-2026-07-30.md` |
| 产品方向与待解决问题 | `../product-notes.md` |
| 两个旧前端怎么退役 | `../LEGACY-FRONTEND-RETIREMENT.md` |

## 注意

归档文档里指向 `docs/` 根的相对链接会多一层(需要 `../`)。没有逐个改 ——
这些文件不再维护,改链接反而会让 `git log` 里多出无意义的改动。
