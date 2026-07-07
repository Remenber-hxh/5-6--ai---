# 智巡管理后台 · 新版(admin-web)

React 18 + Vite 5 + TypeScript + Ant Design v5 + Motion + Zustand + ECharts。
与旧版 `admin-frontend/`(vanilla)并行运行,共用同一 Go 后端与数据库,可随时切换/回退。

## 开发

```bash
npm install
npm run dev      # http://localhost:18090,/api 等代理到本地 go-backend(18080)
npm run build    # 严格 TS 检查 + 产物输出 dist/
```

登录账号与旧版一致(本地默认管理员见 go-backend/cmd/server/identity.go)。

## 部署

- `dist/` 已提交入库(服务器无需 node):服务器 `git pull` 后由 nginx 直接托管。
- nginx 已配 `location /v2/` → `admin-web/dist`(docker-compose.prod.yml 挂卷),
  线上地址 = `https://域名/v2/`。HashRouter,无需 history 回退。
- 改代码后必须本地 `npm run build` 并把 dist 一起提交,否则线上不会更新。

## 结构

```
src/
  api/client.ts    fetch 封装:X-InspectAI-Token、401 踢登录
  api/mgmt.ts      全部业务 API(契约与旧版 admin-frontend 一一对应,勿改字段名)
  store/auth.ts    登录态(zustand);isMgmtRole 与旧版角色门控口径一致
  store/ui.ts      全局项目筛选
  lib/status.ts    recordBusinessStatus(业务状态口径,与旧版逐行一致)/fmtTime/mediaUrl
  lib/history.ts   Agent 聊天历史(localStorage 7 天)
  lib/wordExport.ts 周报/日报导出 Word(MSO HTML .doc)
  lib/csv.ts       CSV 导出(带 BOM)
  lib/live2d.ts    看板娘单例(模型自托管 public/live2d,只在 Agent 页显示)
  pages/           十个页面 + Login
```

## 约定

- 业务状态只用:正常 / 异常 / 待复核 / 需补图 / 人工填写 / 已完成。
- 红色只给真正异常;主色青绿 #12a968;动画只用于等待、状态变化、入场。
- 外网 CDN 不可依赖(本机代理会拦),前端资源一律自托管。
