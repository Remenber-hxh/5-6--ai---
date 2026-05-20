# 方案文件夹索引

两天内交付第一版（截止 2026-05-13 演示，竞赛硬截止 2026-05-15）。

| 文件 | 内容 |
| --- | --- |
| `01-方案总览.md` | 业务流程、识别失败重拍、人工修正、AI 总结追加、台账设计、千问接入、移动端方案 |
| `02-表单字段清单.md` | 10 个真实企业微信表单的字段全集，AI 识别覆盖度评估 |
| `03-需要你提供.md` | 我这边等你给的素材清单（案例图、字段口径、台账粒度等） |
| `04-目录与命名建议.md` | 现有目录混乱的整理方案 + 新短名 `inspectai/` 的规范 |
| `05-倒排时间表.md` | 两天日程，到小时颗粒度 |
| **`06-场景收敛.md`** | **看图后的范围收敛 — 第一版只做 3 主 + 2 辅场景** |
| **`07-给-codex-的讨论清单.md`** | **10 个待对齐的设计选择，codex 已答 + 进度同步** |
| **`08-codex-第一版-review.md`** | **Claude 对 codex 第一版代码的 review，5 必修 + 4 建议 + 5 可优化** |
| **`09-sqlite-schema与store接口.md`** | **SQLite 落库的完整 schema + Go Store 接口设计，给 codex 直接照着实现** |
| **`10-B-A-C-实施计划.md`** | **三件大改的合并实施清单：真 AI 接通 / 拍照优先 / 企微视觉，逐文件逐行号** |
| **`11-密钥加密方案.md`** | **DASHSCOPE_API_KEY 用 Windows DPAPI 加密落盘，不入明文文件，启动脚本自动解密** |
| `12-资产台账数据沉淀与编辑说明.md` | 台账写入流程、字段口径，编辑能力暂未实现 |
| `13-云服务器迁移准备方案.md` | 上云前要补的 Docker / Nginx / 域名清单 |
| `14-Claude代码审计与框架解读.md` | codex → Claude 接管后的全栈 review，9 条问题分 P0/P1/P2 |
| `15-Claude现场问题反馈-转人工与重拍.md` | 5/12 现场操作中发现的 UX 问题 |
| **`16-Claude-当前架构与目录总览.md`** | **现行版本的真相来源（替代过时的 inspectai/README.md）** |
| **`17-Codex-二次复查与交付建议.md`** | **Codex 对当前目录、运行状态、台账能力、上云迁移风险的二次复查建议** |
| **`18-Docker与云部署迭代方案.md`** | **5/15 后上云的迭代依据：Dockerfile 补依赖、生产 compose、密钥换思路、Nginx HTTPS、企微域名清单** |
| **`19-台账与项目管理重构方案.md`** | **台账、项目管理、后台管理、长期维护、数据库演进和运维留痕的详细重构方案** |
| **`20-智能穿戴设备接入与巡检方案书.md`** | **后期接入智能眼镜、记录仪、手表、BLE 传感器的接口、巡检流程、数据库和维护方案** |
| **`21-前端演示视觉优化建议.md`** | **面向领导演示的前端视觉升级建议：第一屏、AI识别中、台账总览、资产详情、审批页和桌面投屏包装** |
| **`22-紫涵雅集数据库对象图与台账边界设计.md`** | **收敛到紫涵雅集两个场景：能耗抄表、综合巡检；重新定义巡检记录、资产台账、资产观测明细和数据库对象图** |
| **`mockups/wework-style.html`** | **企业微信日报视觉参考稿，浏览器直接打开，含 5 个场景 + CSS 变量** |
| `inspectai/ai-service/prompts/*.md` | **6 个 prompt 文件 + README**：_common / energy_meter / screen_reading / paper_form / summary / scene_classifier |

## 一句话现状（2026-05-12 14:30 更新）

- Claude 已接管主程序（详见 [`14-`](14-Claude代码审计与框架解读.md) 之后），codex 第一版归档到 `_archive/InspectAI-Assistant/`。
- 全链路通：classify ~3s / analyze 8s / summarize 5s（qwen-vl-plus + qwen-plus 都跑真 API）。
- DashScope 新 key 已 DPAPI 加密落 `.env.secure`。
- 前端 modal/footer/scene 切换 CSS bug 已修，"从相册上传"已加。
- codex 提的 P0/P1 问题已修：mock 严格化 + submit 校验+幂等 + 图片 ID 解析 + UpsertAsset 错误返回。
- **当前架构与目录的权威记录是 [`16-Claude-当前架构与目录总览.md`](16-Claude-当前架构与目录总览.md)。**

## 阅读顺序

1. **新接手第一次读**：`16-Claude-当前架构与目录总览.md`（一篇看完所有）。
2. **看历史决策**：按 `01 → 06 → 07 → 08` 顺序，能看到 codex 协作期的方案演化。
3. **看接管后改了啥**：`14 → 15 → 16`，含审计、UX 反馈、最终架构。

## 在 plan/ 之外的其他重要目录（2026-05-12 整理后）

| 路径 | 用途 |
| --- | --- |
| `inspectai/` | 主程序，Claude 接管后的当前实现（端口 8080+9100 在跑） |
| `docs/legacy/` | 4/28 早期 3 份 .md（VLM 接入说明 / 批注修订 / 工程逻辑设计） |
| `samples/templates/` | 11 个业务方提供的 Excel 巡检表模板 |
| `samples/photos/` | 巡检真实案例图（原"汇报附件 5.6"，多个场景子目录） |
| `samples/misc/` | report_material.docx + tmp_signup_form.xlsx |
| `_archive/InspectAI-Assistant/` | codex 第一版完整快照，归档保留 |
