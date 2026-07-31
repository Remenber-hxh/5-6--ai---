# 两个旧前端的退役方案

> 状态:**待评审**(2026-07-30)。这份是计划,一行都还没执行。
> 范围:`admin-frontend/`(旧管理后台)与 `frontend/`(旧移动端)。

## 结论先行

**这是两件事,风险差一个数量级,不要打包做。**

| | 旧后台 `/admin/` | 旧移动端 `/` |
|---|---|---|
| 谁在用 | 主管/管理员,坐在办公室 | 巡检员,在现场、弱网、戴手套 |
| 新版验收程度 | 功能已对齐,线上跑在 `/v2/` | **从未部署过** |
| 出事的后果 | 换个地址重新登录 | 当天巡检数据可能丢 |
| 建议 | 本周可做 | 至少两周,且必须先真机试点 |

后台退役基本是白捡的:功能已经齐了,企微通知的链接**早就指向新版**,
只剩书签还指着旧地址。移动端则相反 —— 新版功能更多,但**一天生产流量都没跑过**。

---

## 一、事实基础(逐条查过,不是推测)

### 1.1 线上现在是什么样

`nginx/nginx.conf`:

```
upstream inspectai_admin { server admin-frontend:80; }

location /admin/ { proxy_pass http://inspectai_admin/; }   # 旧后台,主路由
location /v2/    { alias /usr/share/nginx/admin-web/; }    # 新后台,次要路径
location /       { proxy_pass http://inspectai_backend; }  # 后端兜底,顺带发旧移动端
```

旧移动端不是 nginx 直接发的,是 Go 后端按 `FRONTEND_DIR` 环境变量发的
(`docker-compose.prod.yml:131` → `/app/frontend`,实现见 `handlers.go:2520 serveStatic`)。

**这条很关键:两个切换都是改配置,不用重建镜像,回滚一条命令。**

### 1.2 后台功能已经对齐

旧后台 11 个页面(`admin-frontend/index.html` 的 `data-page`):
dashboard / plan / record / ledger / approval / data / profile / users / logs / system / prompts

新后台 `admin-web/src/App.tsx` 全部覆盖 —— 其中 `dashboard` 对应根路由的 `AgentHome`。
**没有缺口。**

### 1.3 企微通知早就不走旧后台了

`notifications.go:98-99` 写得很明白:

> v2 用 HashRouter,路由形如 `https://<host>/v2/#/approval?focus=xxx`。
> 旧版 `/admin/?page=xxx` 保留但不再作为入口。

也就是说主管点企微消息进来的,**已经落在新后台**。旧 `/admin/` 只剩两种人能到:
存了书签的,和手输地址的。

### 1.4 移动端功能也齐了,但没部署过

旧版 10 个场景(`frontend/index.html` 的 `scene*`):
Login / Camera / Loading / Classify / Form / Preview / Tasks / Ledger / Asset / Approvals

新版 `mobile-web` 全部覆盖,另有 `/review`(选照片)、`/me`,以及旧版没有的
离线仓库、多图拍摄、系统水印、批量删除。

**但是:** `mobile-web` 没有 Dockerfile,`docker-compose.prod.yml`、`nginx.conf`、
`scripts/build-images.sh` 里全都没有它。部署这一层是零。

### 1.5 静态前端的部署模式(照抄即可)

`admin-web` 不进镜像。它是**本地构建 → `dist/` 提交进仓库 → nginx 挂 volume**:

```yaml
- ./admin-web/dist:/usr/share/nginx/admin-web:ro   # compose.prod:204
```

注释写了理由:「构建后 commit 进仓库,服务器无需 node」。仓库里现有 85 个 dist 文件。
`mobile-web/.gitignore` 现在忽略 `dist`,要走同样的路子得开个例外。

---

## 二、旧后台退役(低风险,建议先做)

### 第 1 步:`/admin/` 改成跳转,不是删除

```nginx
location /admin/ {
  # 老链接带 ?page=approval 的,救到新版对应路由;其余进首页
  if ($arg_page) { return 301 /v2/#/$arg_page; }
  return 301 /v2/;
}
```

书签还能用,点进去落到新后台。**旧容器先不动**,出事把这段换回 `proxy_pass` 即可。

- 生效方式:`nginx -s reload`,不重建、不停机
- 回滚:改回一行,再 reload

### 第 2 步:观察一周

看什么:

- nginx access log 里 `/admin/` 的命中量。降到接近 0 说明没人还在走老路。
- 有没有人来说「后台打不开了」。主管日常路径是企微消息,理论上无感。

### 第 3 步:摘掉容器

确认第 2 步无异常后,从 `docker-compose.prod.yml` 删掉 `admin-frontend` 服务、
从 `nginx.conf` 删掉 `upstream inspectai_admin`、从 `scripts/build-images.sh` 和
`package-release.ps1` 里去掉它。**这一步要重新部署**,安排在低峰。

### 第 4 步:删目录

单独一个 commit 删 `inspectai/admin-frontend/`(8.9MB)。git 历史留着,真要找回来
`git checkout <sha> -- inspectai/admin-frontend` 即可。

**顺带**:`/v2/` 这个路径名是过渡期的产物。旧的退了之后建议把新后台挪到 `/admin/`,
并保留 `/v2/ → /admin/` 的跳转(企微历史消息里的链接还指着 `/v2/`)。
这一步不急,可以晚几周再做,但别忘了 —— 否则「v2」这个名字会永久留下来。

---

## 三、旧移动端退役(高风险,不能急)

### 为什么要慢

新移动端功能更全,代码也更干净,但它**一天真实生产流量都没跑过**。旧版经过了几个月
现场使用。这中间的差距不是靠看代码能补的,尤其这三件事:

1. **企业微信内嵌浏览器不是 Chrome。** 新移动端重度依赖 IndexedDB(离线仓库)、
   `navigator.storage.persist()`、`<input capture>` 调相机。这些在企微 webview 里
   的行为**必须真机验证**。IndexedDB 若被限制,离线照片会**静默丢失** ——
   这是整套方案里最可怕的一条,因为它不报错。
2. **弱网/离线是常态,不是边界情况。** 领导提这个需求就是因为现场信号差。
   离线仓库、上传队列、退避重试这套东西,只有在真现场才测得出来。
3. **巡检员戴手套、在颠簸环境操作。** 新首页把「取景框即快门」(整框可点)就是为这个
   设计的,但设计意图对不对得真人用了才知道。

### 阶段 0:让新版可达,但不动旧版(零风险)

给 `mobile-web` 加一条独立路径,和旧版并存:

```nginx
location /m2/ { alias /usr/share/nginx/mobile-web/; }
```

配套:

- `mobile-web/.gitignore` 给 `dist` 开例外(照 `admin-web` 的模式)
- `vite.config.ts` 的 `base` 从 `"./"` 确认能在 `/m2/` 子路径下工作
- `docker-compose.prod.yml` 加一行 volume 挂载
- 后端 CORS/Cookie 作用域检查:新旧同域不同路径,cookie 路径别写死成 `/`
  导致互踩登录态

这一步**完全不影响现有巡检员**,他们的地址没变。

### 阶段 1:小范围真机试点(至少一周)

找 1–2 个巡检员,用 `/m2/` 走一周真实工作。重点收集:

- [ ] 企微里能不能正常打开、登录态保不保得住
- [ ] 弱网/断网现场拍照,照片进没进离线仓库
- [ ] 恢复信号后,离线照片有没有**全部**补传成功(数量对得上)
- [ ] 水印、多图、批量删除在真机上的表现
- [ ] 手套 / 强阳光下能不能看清、点得准

**验收线:一周内离线照片零丢失。** 这条不过,不进下一阶段。

### 阶段 2:切主路由,旧版留后路

```yaml
FRONTEND_DIR: "/app/mobile-web-dist"   # compose.prod:131,改这一行
```

同时保留旧版在 `/m1/`,并**当天口头通知巡检员**:出问题就去 `/m1/`。

- 回滚:改回 `FRONTEND_DIR`,重启 go-backend 容器(秒级,不重建镜像)
- 观察:两周

### 阶段 3:清理

两周无异常后,删 `/m1/`、删 `inspectai/frontend/`(8.7MB)、去掉 `FRONTEND_DIR` 的旧默认值。

---

## 四、明确不做的事

- **不在同一次变更里动两个前端。** 出了问题分不清是谁的锅。
- **不在比赛/演示/汇报前一周切移动端。** 后台可以,移动端不行。
- **不靠"看起来没问题"就切。** 阶段 1 的离线零丢失是硬门槛。
- **不删 git 历史。** 目录删掉就行,历史留着,随时能捞回来。

## 五、我的建议排期

| 时间 | 做什么 | 风险 |
|---|---|---|
| 本周 | 后台第 1–2 步(跳转 + 观察) | 低 |
| 下周 | 后台第 3–4 步(摘容器 + 删目录);同时做移动端阶段 0 | 低 |
| 第 3 周 | 移动端阶段 1 试点 | 中(只影响试点的人) |
| 第 4–5 周 | 移动端阶段 2 切换 + 观察 | 高 |
| 第 6 周 | 移动端阶段 3 清理 | 低 |

后台那半边可以立刻开始,和移动端互不干扰。
