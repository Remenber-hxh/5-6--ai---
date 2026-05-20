# InspectAI Assistant

AI巡检填报助手第一版本地原型。目标是把“拍照巡检、AI识别、人工确认、日报提交、资产台账”串成一条可演示的闭环。

## 当前流程

```text
选择点位 -> 拍照上传 -> AI识别日报字段 -> 识别失败弹窗重拍 -> 三次失败转人工填写 -> 人工确认/修改字段 -> 生成AI总结 -> 提交记录 -> 更新资产台账
```

第一版边界：

- 不做全自动巡检，最终结果以人工确认字段为准。
- 不做离线模式和复杂设备台账导入。
- 不让AI覆盖人工修正字段，AI总结单独追加。
- 默认使用本地 Mock 模型，便于无外网、无密钥时演示。

## 本地启动

PowerShell 执行：

```powershell
cd "D:\5-6月 ai 大会\inspectai"
.\scripts\start-local.ps1
```

浏览器访问：

```text
http://127.0.0.1:8080
```

停止服务：

```powershell
.\scripts\stop-local.ps1
```

服务端口：

- Go后端与前端入口：`http://127.0.0.1:8080`
- Python AI微服务：`http://127.0.0.1:9100`

健康检查：

```text
GET http://127.0.0.1:8080/health
GET http://127.0.0.1:9100/health
```

## 真实模型接入

复制 `.env.example` 为 `.env`，不要把真实密钥写进代码或文档。

本地演示默认：

```env
DEMO_MODE=true
AI_PROVIDER=mock
```

接入千问视觉模型：

```env
DEMO_MODE=false
AI_PROVIDER=qwen
DASHSCOPE_API_KEY=你的DashScope密钥
DASHSCOPE_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
QWEN_VISION_MODEL=qwen-vl-plus
```

说明：当前已预留 DashScope OpenAI-compatible 调用骨架。若千问调用失败，AI微服务会自动回退 Mock，保证演示流程不中断。

## 企业微信准备

正式接企业微信后台时，需要准备一个可 HTTPS 访问的可信域名，例如：

```text
https://inspectai.example.com
```

域名侧要完成：

- DNS解析到服务器、网关、CDN或负载均衡。
- 服务器或Nginx部署HTTPS证书。
- 企业微信后台可信域名校验文件放到站点根目录。
- 企业微信后台配置可信域名、应用入口URL和必要的可信IP。

## 目录结构

```text
inspectai/
  ai-service/          Python AI微服务，负责图片识别与模型调用
  frontend/            移动端优先的本地操作页面
  go-backend/          Go业务后端，负责记录、字段、任务、台账
  nginx/               Nginx接入层示例
  scripts/             本地启动与停止脚本
  docker-compose.yml   容器编排示例
  .env.example         环境变量示例
```

## 已知缺口

- 紫涵雅集能耗抄表的历史导出主要是照片字段，缺少独立读数字段，需要确认最终日报是否提交读数。
- 电梯和扶梯需要补充正式设备编号清单，资产台账才能稳定归档到具体设备。
- 当前台账使用内存数据，重启后会清空；后续建议落到 SQLite 或 PostgreSQL。
