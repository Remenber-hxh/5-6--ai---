# 本地启动清单

新前端(mobile-web)+ 新后端(go-backend)开发时,需要跑起来的东西。

---

## 一图看懂

```
手机/浏览器
    │
    ├─→ :18091  mobile-web    新移动端(React)   ← 巡检员用
    ├─→ :18090  admin-web     新管理后台(React) ← 主管/管理员用
    │              │
    │              └──── 都通过 Vite 代理转发 /api ────┐
    │                                                  ↓
    └─→ :18080  go-backend    业务主干(Go)  ←── 旧移动端也挂在这
                   │
                   ├─→ :19100  ai-service  AI 调用层(Python)
                   └─→ :3306   MySQL       数据库
```

**前端不直接连数据库**,全部经 go-backend。Vite dev 会把 `/api` 代理到 18080,
所以本地开发不存在跨域问题。

---

## 依赖顺序(必须按这个顺序起)

| 顺序 | 服务 | 端口 | 少了它会怎样 |
|---|---|---|---|
| 1 | **MySQL** | 3306 | 后端起不来,直接报 ping 失败 |
| 2 | **go-backend** | 18080 | 前端全部 401/无数据,登录不了 |
| 3 | **ai-service** | 19100 | 能登录能拍照,但识别一律失败 |
| 4 | mobile-web / admin-web | 18091 / 18090 | 对应端打不开 |

---

## 一、最省事:一条命令起前三个

```bash
powershell -ExecutionPolicy Bypass -File "D:\5-6月 ai 大会\inspectai\scripts\start-all.ps1"
```

这条会起 **go-backend + ai-service + admin-web**。
**注意:它不含 mobile-web**,新移动端要单独起(见下)。

---

## 二、新移动端 mobile-web(:18091)

```bash
cd "D:\5-6月 ai 大会\inspectai\mobile-web"
npm run dev -- --host
```

`--host` 是为了**手机能连**。不加只能本机访问。

启动后:

| 在哪看 | 地址 |
|---|---|
| 电脑(按 F12 切手机视图) | <http://localhost:18091> |
| **手机**(需与电脑同 WiFi) | `http://<电脑局域网IP>:18091` |

电脑局域网 IP 在启动日志的 `Network:` 那几行里,挑 `192.168.x.x` 或 `10.x.x.x` 那个。

> 首次拉代码后要先 `npm install`。

---

## 三、单独起某一个

### MySQL(开机不自启,常常要手动开)

```bash
net start mysql
```

**需要管理员身份的 PowerShell**。后端报 `ping mysql ... 拒绝` 就是它没起。

### go-backend + ai-service

```bash
powershell -ExecutionPolicy Bypass -File "D:\5-6月 ai 大会\inspectai\scripts\start-local.ps1"
```

改了 Go 代码必须重跑这条才生效(Go 是编译型,不像前端热更新)。

### admin-web 管理后台(:18090)

```bash
cd "D:\5-6月 ai 大会\inspectai\admin-web"
npm run dev
```

---

## 四、确认都活着

```bash
curl -s http://127.0.0.1:18080/health
curl -s http://127.0.0.1:19100/health
```

后端健康返回里 `"storeKind":"mysql"` 表示数据库连上了。

浏览器直接开这几个也行:

- 新移动端 <http://localhost:18091>
- 新后台 <http://localhost:18090>
- 旧移动端 <http://localhost:18080>

> admin-web 的 Vite 只监听 IPv6 回环,用 `localhost` 能开、
> 用 `127.0.0.1` 可能不通 —— 不是服务挂了。

---

## 五、改了代码要不要重启

| 改了什么 | 要做什么 |
|---|---|
| `mobile-web/` 或 `admin-web/` 的前端代码 | **不用**,Vite 热更新;手机上刷新一下 |
| `go-backend/` 的 Go 代码 | **重跑 start-local.ps1** |
| `ai-service/` 的 Python 代码 | 同上 |
| 提示词模板(数据库里) | 不用重启,后台改完即生效 |

---

## 六、常见卡点

**后端起不来,日志报 `ping mysql: ... 拒绝`**
→ MySQL 没启动。管理员 PowerShell 跑 `net start mysql`。

**端口被占用**
```bash
netstat -ano | findstr :18091
taskkill /PID <上面查到的PID> /F
```

**Vite 说端口被占用,自动换了端口**
它会在日志里打印实际用的端口(可能是 18092),按日志里的地址访问。

**手机打不开**
1. 确认启动时加了 `--host`
2. 确认手机和电脑在**同一个 WiFi**
3. Windows 防火墙可能拦了,首次访问要允许

**识别一直失败,但登录正常**
→ ai-service(19100)没起,或模型额度用完了。

---

## 七、账号

| 账号 | 密码 | 说明 |
|---|---|---|
| `admin` | `InspectAI@2026` | 系统管理员,能进后台 |

移动端和后台**用同一套账号**。登录后按角色自动落到对应界面:
巡检员→拍照台,主管→审批,管理员→个人页。

---

## 日志在哪

```
inspectai/storage/logs/
├── go-backend-18080.log      后端标准输出
├── go-backend-18080.log.err  后端错误(起不来先看这个)
├── ai-service-19100.log
└── ai-service-19100.log.err
```
