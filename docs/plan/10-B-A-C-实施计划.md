# B + A + C 三件事实施计划

> 用户决策 2026-05-11 15:30：确认 A、B、C 都要做。
> 优先级：**B → A → C**（先接通真 AI，再改流程，再改视觉）。
> 排期：B = 今晚 + 5/12 上午，A = 5/12 下午，C = 5/12 晚 + 5/13 上午。

## 总览

| 项 | 内容 | Claude 已做 | Codex 要做 | 工时 |
| --- | --- | --- | --- | --- |
| B | 真 AI 接通（字段 + 总结） | 5 个 prompt 文件 | 加载 prompt + /summarize endpoint + main.go 调用 | 2.5h |
| A | 拍照优先 + 场景反推 | scene_classifier.md + 流程稿 | /scene/classify endpoint + 前端流程颠倒 + 兜底手选模板 | 4-5h |
| C | 企业微信日报视觉 | mockups/wework-style.html | 按 mockup 重写 styles.css + index.html 结构调整 | 5h |

总计：12-13h，5/12 全天 + 5/13 上午能干完，下午 demo。

---

## B. 真 AI 接通（最优先，2.5h）

### B1. 按模板 ID 加载 prompt（30 分钟）

**位置**：`inspectai/ai-service/run.py` 现有 `analyze_with_qwen()` (L142)

**当前问题**：所有模板共用 L164-176 写死的通用 prompt。

**改法**（参考 `prompts/README.md` 的加载策略）：

```python
import os

PROMPTS_DIR = os.path.join(os.path.dirname(__file__), "prompts")

def _load(name):
    with open(os.path.join(PROMPTS_DIR, f"{name}.md"), encoding="utf-8") as f:
        return f.read()

COMMON_PROMPT = _load("_common")

TEMPLATE_PROMPTS = {
    "zihan_energy":   _load("energy_meter"),
    "zihan_daily":    _load("screen_reading"),
    "hot_water_room": _load("screen_reading"),
}

def prompt_for_template(template_id, paper_ocr=False):
    if paper_ocr:
        return _load("paper_form")
    return TEMPLATE_PROMPTS.get(template_id)

def analyze_with_qwen(payload):
    template = payload.get("template") or {}
    template_id = template.get("id")
    paper_ocr = payload.get("paperOCR", False)
    
    scenario_prompt = prompt_for_template(template_id, paper_ocr)
    if not scenario_prompt:
        # 该模板第一版没接 prompt → 直接返回 manual_required
        return manual_required_response("当前模板暂未启用 AI 识别，请直接人工填写")
    
    field_lines = format_fields_for_prompt(template.get("fields") or [])
    user_text = (
        scenario_prompt
        + "\n\n字段清单：\n" + field_lines
        + f"\n\ncurrent_date: {datetime.now().strftime('%Y-%m-%d')}"
    )
    
    content = [{"type": "text", "text": user_text}]
    for image in (payload.get("images") or [])[:6]:  # 多图，最多 6 张
        data_url = image_to_data_url(image.get("path") or image.get("Path") or "")
        if data_url:
            content.append({"type": "image_url", "image_url": {"url": data_url}})
    
    if len(content) == 1:
        raise RuntimeError("no readable image for qwen vision call")
    
    # system 消息用 _common.md
    response = call_openai_compatible_chat_with_system(
        model_name=os.environ.get("QWEN_VISION_MODEL", "qwen-vl-plus"),
        system=COMMON_PROMPT,
        user_content=content,
        api_key=...,
    )
    parsed = parse_json_response(response)
    return build_analysis_from_model(...)

def format_fields_for_prompt(fields):
    lines = []
    for f in fields:
        opts = f.get("options") or []
        opts_str = f"  options={opts}" if opts else ""
        lines.append(
            f"- code={f['code']}  label={f['label']}  kind={f['kind']}"
            f"  required={f.get('required', False)}{opts_str}"
        )
    return "\n".join(lines)
```

`call_openai_compatible_chat()` 已存在，扩展成支持 system 参数（或新建一个 `call_openai_compatible_chat_with_system()`）。

### B2. 加 `/summarize` endpoint（1h）

**位置**：`inspectai/ai-service/run.py` 在 `do_POST` 里加一个分支

```python
def do_POST(self):
    if self.path == "/analyze":
        ...  # 现有
    elif self.path == "/summarize":
        try:
            payload = read_json(self)
            write_json(self, 200, summarize_with_qwen(payload))
        except Exception as exc:
            write_json(self, 500, {"error": "summary_failed", "message": str(exc)})
    else:
        write_json(self, 404, {"error": "not_found"})

SUMMARY_PROMPT = _load("summary")

def summarize_with_qwen(payload):
    """
    payload schema (Go 后端传过来):
    {
      "templateName": "...",
      "project": "...",
      "pointName": "...",
      "inspector": "...",
      "inspectionTime": "...",
      "fields": [{"label": "...", "value": "..."}],
      "abnormalNote": "",
      "history": {
        "lastInspectionTime": "...",
        "lastFields": [{"label": "...", "value": "..."}]
      } | None    # 可选，第一次巡检该资产时为 None
    }

    返回 schema:
    {
      "summary": "事实总结正文",
      "tags": ["正常" / "异常待跟进" / "部分待复核"],
      "recommendations": [
        {
          "priority": "high"|"medium"|"low",
          "category": "异常处理"|"趋势预警"|"数据补全"|"下次巡检关注",
          "text": "建议正文",
          "basis": "依据"
        }
      ]
    }
    """
    api_key = os.environ.get("DASHSCOPE_API_KEY", "").strip()
    if not api_key:
        # mock 兜底，让本地演示也能看到效果
        return {
            "summary": _mock_summary(payload),
            "tags": ["正常"],
            "recommendations": [],
            "model": "mock",
        }
    
    text_model = os.environ.get("QWEN_TEXT_MODEL", "qwen-plus")
    user_text = json.dumps(payload, ensure_ascii=False)
    
    response = call_openai_compatible_chat_text(
        model=text_model,
        system=SUMMARY_PROMPT,
        user_text=user_text,
        api_key=api_key,
        timeout=8,   # 总结 + 建议比纯总结慢，给到 8s
    )
    parsed = parse_json_response(response)
    return {
        "summary": parsed.get("summary", ""),
        "tags": parsed.get("tags", []),
        "recommendations": _normalize_recommendations(parsed.get("recommendations", [])),
        "model": text_model,
    }

def _normalize_recommendations(raw):
    """限制最多 3 条，priority 校验，缺字段补默认。"""
    out = []
    for r in (raw or [])[:3]:
        if not isinstance(r, dict): continue
        priority = r.get("priority", "low")
        if priority not in ("high", "medium", "low"):
            priority = "low"
        out.append({
            "priority": priority,
            "category": r.get("category", "下次巡检关注"),
            "text": str(r.get("text", "")).strip(),
            "basis": str(r.get("basis", "")).strip(),
        })
    return out
```

### B3. main.go submitRecord 调 /summarize（1h）

**位置**：`go-backend/cmd/server/main.go` L969-990 `submitRecord()`

**改法**：在 `rec.AISummary = buildAISummary(rec)` 那行替换为调 ai-service：

```go
func (s *Server) submitRecord(w http.ResponseWriter, r *http.Request, recordID string) {
    key := r.Header.Get("Idempotency-Key")
    if key == "" {
        writeError(w, http.StatusBadRequest, "missing_idempotency_key", "提交记录需要 Idempotency-Key")
        return
    }
    now := time.Now()

    s.store.mu.Lock()
    rec := s.store.records[recordID]
    if rec == nil {
        s.store.mu.Unlock()
        writeError(w, http.StatusNotFound, "record_not_found", "巡检记录不存在")
        return
    }
    s.store.mu.Unlock()

    // 调 AI 总结（同步，5s 超时）
    summary, summaryErr := s.callSummarize(rec)
    
    s.store.mu.Lock()
    defer s.store.mu.Unlock()
    rec = s.store.records[recordID]
    if summaryErr != nil {
        rec.AISummary = buildAISummary(rec)  // 兜底用本地拼接
        rec.AIRecommendations = nil          // 兜底时建议留空，不要假装有 AI 建议
        rec.AISummaryError = summaryErr.Error()
    } else {
        rec.AISummary = summary.Summary
        rec.AISummaryTags = summary.Tags
        rec.AIRecommendations = summary.Recommendations
    }
    rec.Report = buildDailyPreview(rec)
    rec.Submitted = true
    rec.SubmittedAt = &now
    s.upsertAssetLocked(rec, now)
    writeJSON(w, http.StatusOK, rec)
}

type SummaryResult struct {
    Summary         string           `json:"summary"`
    Tags            []string         `json:"tags"`
    Recommendations []Recommendation `json:"recommendations"`
}

type Recommendation struct {
    Priority string `json:"priority"`  // high / medium / low
    Category string `json:"category"`  // 异常处理 / 趋势预警 / 数据补全 / 下次巡检关注
    Text     string `json:"text"`
    Basis    string `json:"basis"`
}

func (s *Server) callSummarize(rec *Record) (*SummaryResult, error) {
    payload := map[string]interface{}{
        "templateName":   rec.TemplateName,
        "project":        rec.Project,
        "pointName":      rec.PointName,
        "inspector":      rec.Inspector,
        "inspectionTime": rec.CreatedAt.Format("2006-01-02 15:04"),
        "fields":         simplifyFieldsForSummary(rec.Fields),
        "history":        s.lookupAssetHistory(rec),  // 见下方
    }
    body, _ := json.Marshal(payload)
    req, _ := http.NewRequest("POST", s.aiURL+"/summarize", bytes.NewReader(body))
    req.Header.Set("Content-Type", "application/json")
    
    client := &http.Client{Timeout: 10 * time.Second}  // 总结+建议比纯总结慢
    resp, err := client.Do(req)
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    if resp.StatusCode >= 300 {
        return nil, fmt.Errorf("summarize status %d", resp.StatusCode)
    }
    var result SummaryResult
    if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
        return nil, err
    }
    return &result, nil
}

// 从台账或最近的同资产记录里取上次的字段值，喂给 AI 让它做对比建议
// SQLite 落库后这个函数才有意义；MemStore 阶段返回 nil 即可
func (s *Server) lookupAssetHistory(rec *Record) interface{} {
    assetID := assetID(rec, fieldValue(rec.Fields, "asset_no"))
    asset, _ := s.store.GetAsset(assetID)
    if asset == nil || asset.LastRecordID == "" || asset.LastRecordID == rec.ID {
        return nil  // 没有历史，或者历史就是自己
    }
    last, _ := s.store.GetRecord(asset.LastRecordID)
    if last == nil {
        return nil
    }
    return map[string]interface{}{
        "lastInspectionTime": last.CreatedAt.Format("2006-01-02 15:04"),
        "lastFields":         simplifyFieldsForSummary(last.Fields),
    }
}

// Record 加字段
type Record struct {
    ...
    AISummary         string           `json:"aiSummary"`
    AISummaryTags     []string         `json:"aiSummaryTags"`
    AIRecommendations []Recommendation `json:"aiRecommendations"`
    AISummaryError    string           `json:"aiSummaryError,omitempty"`
    ...
}

func simplifyFieldsForSummary(fields []FieldValue) []map[string]string {
    out := make([]map[string]string, 0, len(fields))
    for _, f := range fields {
        if strings.TrimSpace(f.Value) == "" {
            continue
        }
        out = append(out, map[string]string{
            "label": f.Label,
            "value": f.Value,
        })
    }
    return out
}
```

### B 验收标准

- [ ] `AI_PROVIDER=qwen` + 真 key 时，能耗抄表上传 6 张电表图，AI 返回的字段对得上
- [ ] 提交后 `aiSummary` 字段是千问生成的，不是 fmt.Sprintf 拼接
- [ ] 提交响应里 `aiRecommendations` 是数组，且每条含 priority/category/text/basis 四字段
- [ ] **建议条数 ≤ 3，且没有"建议加强巡检"这类套话**
- [ ] 同一资产第二次巡检时，AI 建议里出现"较上次..."的对比表达（依赖 lookupAssetHistory 接通）
- [ ] AI 服务挂掉时，前端仍能完成提交（aiSummary 走兜底，aiRecommendations 留空数组，UI 显示"AI 建议暂不可用"）
- [ ] mock 模式下走完整流程不会调外部 API

### B 前端展示要求（按 mockup 场景 5）

提交成功后的"日报预览页"必须显示**两张独立卡片**：

1. **AI 总结卡**（蓝色左 border `--wx-blue`）— 客观事实，一段流水
2. **AI 行动建议卡**（橙色左 border `--wx-orange`）— 列表，每条带：
   - 左侧 priority 标签（高/中/低，配 red/orange/gray 三色）
   - 主文本（建议正文）
   - 灰色 meta 行（category 小 badge + "依据：..."）

如果 `aiRecommendations` 为空数组：建议卡里显示"本次巡检暂无 AI 建议"灰字 placeholder，**不要隐藏卡**（保持视觉结构稳定，让用户知道这是 AI 主动判断"无建议"）。

---

## A. 拍照优先 + 场景反推（4-5h）

### A1. 后端新 endpoint：POST /api/scene/classify（1.5h）

**位置**：`main.go` 加新路由

```go
// router
case r.Method == http.MethodPost && r.URL.Path == "/api/scene/classify":
    s.classifyScene(w, r)

// handler
func (s *Server) classifyScene(w http.ResponseWriter, r *http.Request) {
    if err := r.ParseMultipartForm(32 << 20); err != nil {
        writeError(w, http.StatusBadRequest, "bad_multipart", err.Error())
        return
    }
    files := r.MultipartForm.File["files"]
    if len(files) == 0 {
        writeError(w, http.StatusBadRequest, "no_files", "请先拍照")
        return
    }
    
    // 临时保存图（不绑定 record，分类完了如果用户取消就清理）
    tmpDir := filepath.Join(s.storageDir, "tmp_classify", newID("cls"))
    os.MkdirAll(tmpDir, 0755)
    
    paths := []string{}
    for _, header := range files[:min(len(files), 3)] {  // 只用前 3 张分类
        img, err := s.saveUploadedFileTo(tmpDir, header)
        if err != nil {
            writeError(w, http.StatusBadRequest, "upload_failed", err.Error())
            return
        }
        paths = append(paths, img.Path)
    }
    
    // 调 ai-service /classify
    result, err := s.callClassify(paths)
    if err != nil {
        // 分类失败 → 走"unknown"，让前端弹手选
        writeJSON(w, http.StatusOK, map[string]interface{}{
            "templateId":      "unknown",
            "templateName":    "无法识别",
            "confidence":      0.0,
            "needsManualPick": true,
            "tmpDir":          tmpDir,  // 用户手选后会用到
            "error":           err.Error(),
        })
        return
    }
    result["tmpDir"] = tmpDir
    writeJSON(w, http.StatusOK, result)
}
```

ai-service 端 `/classify`：

```python
SCENE_PROMPT = _load("scene_classifier")

def classify_scene(payload):
    api_key = os.environ.get("DASHSCOPE_API_KEY", "").strip()
    if not api_key:
        # mock：随机返回一个有 prompt 的模板
        return {
            "templateId": "zihan_energy",
            "templateName": "紫涵雅集能耗抄表（mock）",
            "confidence": 0.85,
            "needsManualPick": False,
        }
    
    images = payload.get("images") or []
    content = [{"type": "text", "text": SCENE_PROMPT}]
    for image in images[:3]:
        data_url = image_to_data_url(image.get("path") or "")
        if data_url:
            content.append({"type": "image_url", "image_url": {"url": data_url}})
    
    if len(content) == 1:
        return {"templateId": "unknown", "needsManualPick": True}
    
    # qwen-vl-plus 速度优先
    response = call_openai_compatible_chat(
        os.environ.get("QWEN_VISION_MODEL", "qwen-vl-plus"),
        content, api_key, timeout=8,
    )
    parsed = parse_json_response(response)
    return parsed
```

### A2. 前端流程颠倒（2-3h）

**当前 5 步**：setup → photo → review → submit → ledger
**改成**：camera → classify → confirm template → review → submit → ledger

新流程：
1. **入口（camera）**：默认进入"全屏相机"页面，参考 `mockups/wework-style.html` 场景 1
   - 顶部："对准设备拍照，AI 自动识别场景"
   - 大快门按钮
   - 下方小字"或 [手动选择模板]"
2. **拍照后（classify）**：参考 mockup 场景 2
   - "AI 正在识别场景…"全屏 loading
   - 调 `POST /api/scene/classify`
3. **识别完成**：
   - confidence ≥ 0.7 且非 unknown → 弹底部 sheet"识别为【紫涵能耗抄表】，确认开始填报？" + 按钮"确认 / 重新选"
   - 否则 → 弹模板选择列表（10 个模板，5 个 featured 在上方）
4. **确认模板后**：用 `tmpDir` 里的图直接 createRecord + uploadImages（无需重传）
5. **后续**：与 codex 当前 review/submit/ledger 阶段一致

### A3. 兜底"手动选模板"（30 分钟）

- 入口 1：相机页底部 "或 手动选择模板"
- 入口 2：classify 失败弹窗"AI 不太确定，请手动选"

模板列表 UI（参考 mockup 风格）：
- Featured 5 个（zihan_energy / zihan_daily / hot_water_room / fire_pump / ups_room）显示在上方，大按钮
- 折叠"更多模板"展开剩下 5 个
- 选中后跳到拍照流程（如果 tmpDir 有图直接用，没图再拍）

### A 验收标准

- [ ] 打开 app 默认见相机界面（不是模板列表）
- [ ] 拍能耗表 → AI 识别为 zihan_energy → 确认进入字段确认页
- [ ] 拍墙 → 识别 unknown → 弹手选
- [ ] 手动选模板路径仍能完整跑通

---

## C. 企业微信日报视觉重写（5h）

### C1. 完整参考稿位置

**视觉参考**：`plan/mockups/wework-style.html`（用浏览器直接打开看效果）
- 5 个场景（相机 / 识别中 / 表单 / 失败弹窗 / AI 总结）
- 完整 CSS 变量定义在 `:root` 里，codex 直接复制到 `styles.css`

### C2. 核心配色（替换现有的绿色生态）

```css
:root {
  --wx-blue: #576B95;        /* 企微蓝，主操作色 */
  --wx-blue-dark: #3F5470;   /* 按下态 */
  --wx-bg: #EDEDED;          /* 页面灰底 */
  --wx-bg-card: #FFFFFF;     /* 卡片白底 */
  --wx-text: #191919;        /* 主文本 */
  --wx-text-2: #888888;      /* 次要文本 */
  --wx-text-3: #B2B2B2;      /* placeholder */
  --wx-line: #E5E5E5;        /* 分割线 */
  --wx-line-soft: #F2F2F2;   /* 表单内分割 */
  --wx-red: #FA5151;         /* 必填星号 / 错误 */
  --wx-green: #07C160;       /* 成功 / 正常 */
  --wx-orange: #FA9D3B;      /* 待复核 */
}
```

替换 `styles.css` 的绿色 `--green` 系列。

### C3. 关键结构变化

- **移除**：`<aside class="ops-panel">`（PC 操作面板，整块删掉，timeline 暂时不要）
- **顶部**：把 `.hero` 替换为 mockup 里的 `.topbar`（44px 高，左返回 + 中标题 + 右草稿）
- **stepper**：5 按钮 stepper 改成 mockup 里 3px 高细进度条 `.progress`，根据当前阶段算 width%
- **字段卡片**：`.field-card` 改成 mockup 的 `.field`（表单行：label 左 100px + 输入框右对齐 + AI 置信度小标签）
- **底部按钮**：所有 `.primary-action` 移到 `<div class="footer">` 里 sticky，全宽 44 高 #576B95
- **模态弹窗**：`.modal-card` 套用 mockup 的圆角 320px 宽 + 底部双按钮 split style

### C4. 移动端真 first

`.app` 默认 `max-width: 480px; margin: 0 auto`（PC 上居中显示成一个手机宽度）。
**不要 media query**，PC 视为大屏移动端。
触摸目标全部 ≥44px。

### C 验收标准

- [ ] iPhone Safari 打开看着像企业微信里的填报页（白底 + 蓝按钮 + 表单行）
- [ ] PC Chrome 打开是 480px 居中的"手机模拟器"效果
- [ ] 没有冗余的 PC 侧栏 / stepper 5 按钮 / 卡片堆
- [ ] 失败弹窗、AI 总结卡片样式贴合 mockup

---

## 排期（5/12 - 5/13）

| 时间 | 谁 | 做什么 |
| --- | --- | --- |
| 5/11 晚 | Codex | B1 + B2 + B3（按本文档照做，2.5h） |
| 5/12 上午 | Codex | A1 + A2（4h） |
| 5/12 上午 | Claude | 看 codex B 的实现，跑通真 AI 联调 |
| 5/12 下午 | Codex | A3 + 联调 |
| 5/12 下午 | Claude | 帮 codex 调 prompt，看 AI 真识别效果决定要不要补 prompt |
| 5/12 晚 | Codex | C1-C4（5h，CSS 重写） |
| 5/13 上午 | Codex + Claude | 全链路冒烟 + 失败剧本调试 |
| 5/13 下午 | — | **对内 demo** |

## 验收清单总表

```text
B 真 AI:
  [ ] 字段识别按模板 prompt 走，不是通用 prompt
  [ ] 总结是真千问生成
  [ ] 失败 fallback 不丢字段提交

A 拍照优先:
  [ ] 入口默认相机
  [ ] AI 反推场景准确率 >70%
  [ ] 手选模板兜底可用

C 视觉:
  [ ] 移动端 first
  [ ] 企微蓝 + 白底 + 表单行
  [ ] 底部 sticky 主按钮
```

任意一项打 ❌ 即视为没改完，需要 codex 当场修。

## 不做（明确边界）

- ❌ 真 OAuth 登录企微（演示用 fake 巡检员身份）
- ❌ 离线缓存（PWA / Service Worker）
- ❌ 真台账图表（只做列表 + 按资产折叠）
- ❌ 多角色权限（demo 单角色）
