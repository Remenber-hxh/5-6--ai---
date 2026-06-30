"""InspectAI Assistant - Python AI 微服务

负责：
  POST /classify   - 场景分类（拍照后反推模板）
  POST /analyze    - 字段识别（按模板加载对应 prompt）
  POST /summarize  - 总结生成（事实总结 + 行动建议）
  GET  /health     - 健康检查

依赖：仅 Python 标准库。千问通过 dashscope OpenAI 兼容端点调用。
"""

import base64
import json
import os
import re
import subprocess
import sys
import tempfile
import time
import uuid
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# 让 print 支持中文
sys.stdout.reconfigure(encoding="utf-8", errors="replace")  # type: ignore[attr-defined]

# 注意：Python urllib 在本机连 dashscope.aliyuncs.com 时 SSL 握手会卡死
# （多次尝试 30-60s 全部超时），但 curl -4 在 0.3s 内能通。
# 故 HTTP 调用全部走 subprocess curl，强制 IPv4，回避 Python 网络栈问题。
CURL_PATH = os.environ.get("CURL_PATH", "curl.exe" if sys.platform == "win32" else "curl")


# ===== Prompt 加载 =====

PROMPTS_DIR = os.path.join(os.path.dirname(os.path.abspath(__file__)), "prompts")


def _load_prompt(name: str) -> str:
    path = os.path.join(PROMPTS_DIR, f"{name}.md")
    with open(path, encoding="utf-8") as f:
        return f.read()


COMMON_PROMPT = _load_prompt("_common")
SCENE_PROMPT = _load_prompt("scene_classifier")
SUMMARY_PROMPT = _load_prompt("summary")

# 模板 ID → prompt 文件名
TEMPLATE_PROMPT_MAP = {
    "zihan_energy": "energy_meter",
    "zihan_daily": "screen_reading",
    "hot_water_room": "screen_reading",
    "elevator_no_room": "elevator_no_room",
    "elevator_machine_room": "elevator_machine_room",
    "escalator": "escalator",
    "power_room": "substation",
}


def prompt_for_template(template_id: str, paper_ocr: bool = False):
    if paper_ocr:
        return _load_prompt("paper_form")
    name = TEMPLATE_PROMPT_MAP.get(template_id)
    if not name:
        return None
    return _load_prompt(name)


# ===== 工具 =====


def now_iso() -> str:
    return datetime.now(timezone.utc).astimezone().isoformat()


def write_json(handler: BaseHTTPRequestHandler, status: int, payload: dict):
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json; charset=utf-8")
    handler.send_header("Content-Length", str(len(body)))
    handler.end_headers()
    handler.wfile.write(body)


def read_json(handler: BaseHTTPRequestHandler) -> dict:
    length = int(handler.headers.get("Content-Length", "0") or "0")
    if length == 0:
        return {}
    raw = handler.rfile.read(length)
    return json.loads(raw.decode("utf-8"))


def image_to_data_url(path: str, force_compress: bool = False) -> str:
    if not path or not os.path.exists(path):
        return ""
    ext = os.path.splitext(path)[1].lower().strip(".")
    mime = {
        "jpg": "image/jpeg",
        "jpeg": "image/jpeg",
        "png": "image/png",
        "webp": "image/webp",
    }.get(ext, "image/jpeg")
    with open(path, "rb") as f:
        data = f.read()
    if force_compress or len(data) > 8 * 1024 * 1024:
        # force_compress:分类场景为提速强制压缩;否则仅 >8MB 才压
        compressed = try_compress(data, ext)
        if compressed:
            data = compressed
            mime = "image/jpeg"
    return f"data:{mime};base64,{base64.b64encode(data).decode('ascii')}"


def try_compress(data: bytes, ext: str) -> bytes | None:
    """尝试用 Pillow 压缩到长边 1600 / JPEG 80。Pillow 不可用就返回 None。"""
    try:
        from io import BytesIO

        from PIL import Image  # type: ignore[import-not-found]

        img = Image.open(BytesIO(data))
        img = img.convert("RGB")
        max_edge = 1600
        if max(img.size) > max_edge:
            ratio = max_edge / max(img.size)
            img = img.resize((int(img.size[0] * ratio), int(img.size[1] * ratio)))
        out = BytesIO()
        img.save(out, format="JPEG", quality=80, optimize=True)
        return out.getvalue()
    except ImportError:
        return None
    except Exception as exc:
        print(f"[compress] failed: {exc}", file=sys.stderr)
        return None


def parse_json_response(text: str) -> dict:
    """从模型回复里抽取严格 JSON。处理 markdown 包裹和前后缀。"""
    if not text:
        raise ValueError("empty model response")
    cleaned = text.strip()
    m = re.search(r"```(?:json)?\s*(.*?)```", cleaned, re.S)
    if m:
        cleaned = m.group(1).strip()
    first = cleaned.find("{")
    last = cleaned.rfind("}")
    if first >= 0 and last >= first:
        cleaned = cleaned[first : last + 1]
    return json.loads(cleaned)


# ===== 千问调用 =====


def call_qwen_chat(
    *,
    model: str,
    system: str,
    user_content,  # str 或 list[dict]
    api_key: str,
    timeout: int = 60,
    temperature: float = 0.1,
    max_retries: int = 2,
    extra_body: dict | None = None,
) -> str:
    base_url = os.environ.get(
        "DASHSCOPE_BASE_URL",
        "https://dashscope.aliyuncs.com/compatible-mode/v1",
    ).rstrip("/")
    url = f"{base_url}/chat/completions"

    body = {
        "model": model,
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user_content},
        ],
        "temperature": temperature,
    }
    if extra_body:
        body.update(extra_body)
    body_bytes = json.dumps(body, ensure_ascii=False).encode("utf-8")

    # 写到临时文件后 --data-binary @file，避免命令行长度限制 + quoting 问题
    tmp = tempfile.NamedTemporaryFile("wb", suffix=".json", delete=False)
    try:
        tmp.write(body_bytes)
        tmp.close()
        tmp_path = tmp.name

        last_err: Exception | None = None
        for attempt in range(max_retries + 1):
            try:
                proc = subprocess.run(
                    [
                        CURL_PATH,
                        "-4",            # 强制 IPv4
                        "-s",            # silent
                        "-S",            # show errors on stderr
                        "--noproxy", "*", # 绕开系统代理（Clash/Fiddler 等会拦 dashscope）
                        "-m", str(timeout),
                        "-X", "POST",
                        url,
                        "-H", f"Authorization: Bearer {api_key}",
                        "-H", "Content-Type: application/json",
                        "--data-binary", f"@{tmp_path}",
                    ],
                    capture_output=True,
                    timeout=timeout + 5,
                )
                if proc.returncode != 0:
                    err = proc.stderr.decode("utf-8", errors="replace")[:200]
                    last_err = RuntimeError(f"curl exit {proc.returncode}: {err}")
                    if attempt < max_retries:
                        time.sleep(0.5 * (2 ** attempt))
                        continue
                    raise last_err
                raw = proc.stdout.decode("utf-8", errors="replace")
                payload = json.loads(raw)
                if "error" in payload:
                    err_obj = payload.get("error") or {}
                    msg = err_obj.get("message", "unknown")
                    code = err_obj.get("code") or err_obj.get("type", "error")
                    raise RuntimeError(f"qwen error [{code}]: {msg}")
                choices = payload.get("choices") or []
                if not choices:
                    raise RuntimeError("qwen response has no choices")
                return (choices[0].get("message") or {}).get("content") or ""
            except subprocess.TimeoutExpired:
                last_err = RuntimeError(f"curl timeout after {timeout}s")
                if attempt < max_retries:
                    continue
                raise last_err
        if last_err:
            raise last_err
        raise RuntimeError("qwen call failed after retries")
    finally:
        try:
            os.unlink(tmp_path)
        except Exception:
            pass


def get_api_key() -> str:
    # 优先支持 *_FILE 约定（Docker secret 标准），其次回退到普通环境变量。
    # Why: 生产环境密钥不能明文进环境变量；secret 会挂载到 /run/secrets/<name>，
    # 由 DASHSCOPE_API_KEY_FILE 指向其路径。
    key_file = os.environ.get("DASHSCOPE_API_KEY_FILE", "").strip()
    if key_file:
        try:
            with open(key_file, "r", encoding="utf-8") as fp:
                return fp.read().strip()
        except OSError:
            pass
    return os.environ.get("DASHSCOPE_API_KEY", "").strip()


def get_deepseek_key() -> str:
    """管理 AI(DeepSeek)的 key。Linux 走 *_FILE,Windows 本地走 DPAPI 注入的 env。"""
    key_file = os.environ.get("DEEPSEEK_API_KEY_FILE", "").strip()
    if key_file:
        try:
            with open(key_file, "r", encoding="utf-8") as fp:
                return fp.read().strip()
        except OSError:
            pass
    return os.environ.get("DEEPSEEK_API_KEY", "").strip()


def call_deepseek_chat(
    *,
    model: str,
    system: str,
    user_content: str,
    api_key: str,
    timeout: int = 30,
    temperature: float = 0.2,
    max_retries: int = 1,
) -> tuple[str, str]:
    """打 DeepSeek OpenAI 兼容端点;返回 (reply_text, actual_model)。"""
    base_url = os.environ.get("DEEPSEEK_BASE_URL", "https://api.deepseek.com").rstrip("/")
    url = f"{base_url}/chat/completions"
    body = {
        "model": model,
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user_content},
        ],
        "temperature": temperature,
    }
    body_bytes = json.dumps(body, ensure_ascii=False).encode("utf-8")
    tmp = tempfile.NamedTemporaryFile("wb", suffix=".json", delete=False)
    try:
        tmp.write(body_bytes)
        tmp.close()
        tmp_path = tmp.name
        last_err: Exception | None = None
        for attempt in range(max_retries + 1):
            try:
                proc = subprocess.run(
                    [
                        CURL_PATH, "-4", "-s", "-S",
                        "--noproxy", "*",
                        "-m", str(timeout),
                        "-X", "POST", url,
                        "-H", f"Authorization: Bearer {api_key}",
                        "-H", "Content-Type: application/json",
                        "--data-binary", f"@{tmp_path}",
                    ],
                    capture_output=True,
                    timeout=timeout + 5,
                )
                if proc.returncode != 0:
                    err = proc.stderr.decode("utf-8", errors="replace")[:200]
                    last_err = RuntimeError(f"curl exit {proc.returncode}: {err}")
                    if attempt < max_retries:
                        time.sleep(0.4 * (2 ** attempt))
                        continue
                    raise last_err
                raw = proc.stdout.decode("utf-8", errors="replace")
                payload = json.loads(raw)
                if "error" in payload:
                    err_obj = payload.get("error") or {}
                    msg = err_obj.get("message", "unknown")
                    code = err_obj.get("code") or err_obj.get("type", "error")
                    raise RuntimeError(f"deepseek error [{code}]: {msg}")
                choices = payload.get("choices") or []
                if not choices:
                    raise RuntimeError("deepseek response has no choices")
                content = (choices[0].get("message") or {}).get("content") or ""
                actual_model = payload.get("model") or model
                return content, actual_model
            except subprocess.TimeoutExpired:
                last_err = RuntimeError(f"deepseek curl timeout after {timeout}s")
                if attempt < max_retries:
                    continue
                raise last_err
        if last_err:
            raise last_err
        raise RuntimeError("deepseek call failed after retries")
    finally:
        try:
            os.unlink(tmp_path)
        except Exception:
            pass


# 管理 AI 的系统 prompt — 阶段一边界约束:只回答台账问题,不修改任何数据
MANAGEMENT_CHAT_SYSTEM = """你是「智巡」管理后台的 AI 助手,服务于设施巡检主管,能就「智巡」全系统答疑——
巡检计划、巡检记录、资产台账、审批中心、设备健康、数据看板、用户与权限、操作日志、系统管理、复核质量等所有维度都答。

上下文 JSON 给了其中几类的实时数据,各字段含义:
- overview:整体规模与异常/待复核/待审批计数
- topRiskAssets:风险最高的资产(风险分、异常次数、原因)
- repeatedIssues:重复出现的异常字段(哪台设备哪个字段反复异常、次数)
- pendingReviews:待复核记录(needsReview)+ 待审批申请(pendingApprovals)
- inspectorQuality:各巡检员质量(没看图就确认 noPhotoConfirm、人工修正等)
- numericDrift:数值字段漂移明细(字段在两次巡检间的变化率)

**先读懂管理员到底问什么,再答(本系统任何话题都答,有数据就用下面这几类数据,没有就给简短有帮助的回答):**
- 问"复核率 / 待复核 / 谁没看图就确认" → 用 pendingReviews 和 inspectorQuality 作答
- 问"重复风险 / 反复异常 / 某类设备(如无机房电梯)" → 用 repeatedIssues 作答
- 问"字段漂移 / 数值变化 / 趋势" → 用 numericDrift 作答
- 问宽泛的"重点关注 / 今天优先处理什么" → 以 topRiskAssets 第一名作答
- 问巡检计划/记录/用户权限/操作日志/系统配置等(上下文未给数据)→ 给简短有帮助的回答,别拒答
- **绝不要每个问题都把最高风险那台资产复述一遍**;要紧扣问的那件事。

输出格式:
1. 第一句直接回答这个问题,把最关键处用 **…** 加粗(全文只加粗这一处)。
2. 另起一行写「依据:」,跟 1-2 条短句,每条一个关键数字/事实,只补充结论之外的信息,不复述结论里已说过的数字。
3. 全文 50-90 字,简洁;除那一处加粗外不用其它 markdown。

硬性约束:
- **巡检计划/巡检记录/资产台账/审批/设备/数据看板/用户与权限/操作日志/系统管理/复核质量 等本系统话题,一律视为「相关」,绝对不能回拒答语,哪怕上下文没给该数据。** 没数据时就给一句简短有帮助的回答或方向(例如该看哪一块),严禁在这类回答里出现"请问与…相关的问题"。
- 只有**与设施巡检完全无关**的(天气、闲聊、写代码、常识问答等)才回:"请问与巡检管理相关的问题"。
- 没数据的本系统话题怎么答(照此风格,绝不拒答):
  · 问"操作日志谁改了数据" → "操作日志会留痕每位成员的改动与时间,我这边暂无实时日志明细,可在操作日志里按人或时间筛选查看。"
  · 问"系统怎么配企业微信通知" → "企业微信通知在系统管理里配置(企业 ID / 应用密钥 / Webhook),配好后异常和待办会自动推送。"
  · 问"有哪些用户/权限" → "用户与权限按角色管理(主管 / 巡检员等),我这边暂无人员名单明细,可在用户与权限里查看与分配。"
- 严禁编造数据,只能用上下文里给的数字和名称;对应数据为空就如实说"暂无该数据",不要估算或夸大。
- 你不能直接改数据/审批/派单;但可以按下方规则产出一个"动作提议",由主管一键确认后系统执行。不要写"去 X 菜单点 Y"这类文字导航。
- 一律用可读设备名(如 HYZX-WJ-DT01)和中文字段名(如"按钮显示");绝不输出 rec_xxx、user_xxx 原始 ID 或 buttons_display 英文字段键。

动作提议(仅在确有必要时附,其余情况绝不附):
- **只要存在某台设备反复异常且尚未闭环、适合派一次现场复查**,就在正文之后另起一行,按下面格式附**一个**提议块(正文照常 50-90 字,不受影响)。**包括**回答"今天优先处理什么 / 重点关注哪些设备 / 该怎么处理"这类问题时,只要存在这样的设备就一并附上:
<<ACTION>>
{"type":"create_recheck_task","asset":"设备可读编号","assignee":"责任人(不确定就省略此项)","dueAt":"YYYY-MM-DD(不确定就省略)","reason":"一句话复查理由"}
<<END>>
- asset 必须是上下文里真实出现的可读编号;assignee/dueAt 不知道就不要写那一项,绝不编造;reason 用中文一句话。
- 问"复核率/趋势/谁没看图"等其它问题时,**不要**附动作块。
"""

MANAGEMENT_REPORT_SYSTEM = """你是「智巡」管理 AI 的报告生成器。基于后端聚合好的数据,
为主管写一段「全局态势摘要」,80-180 字,无 markdown,无项目符号,流水句。

包含:
1. 本期巡检规模与异常情况(资产数/巡检数/正常/待复核/异常)
2. 重点关注的 1-3 个资产(从 attention 列表里挑风险最高的)
3. 一个建议性的方向(下次优先关注什么)

不要编造名称或数字;只用 overview/attention 里有的数据。
"""

MANAGEMENT_WEEKLY_SYSTEM = """你是「智巡」管理 AI 的周报撰写助手。基于后端聚合好的"近 7 天"数据,为设施巡检主管写一段「本周态势综述」,120-220 字,无 markdown、无项目符号、无小标题,连贯流水句。

必须覆盖(只用 overview / attention / repeatedIssues / inspectorQuality 里给的数字,绝不编造):
1. 本周巡检规模与环比:recordRecent 本周巡检数 vs recordPrev 上周,abnormalRecent 异常数 vs abnormalPrev。
2. 复核与质量:pendingReviews 待复核、lazyConfirmRate 未看图即确认占比(乘 100 写成百分比)、inspectorQuality 里突出的人。
3. 重点设备:从 attention / repeatedIssues 挑 1-3 个反复异常或风险最高的(用 assetName)。
4. 待办:pendingApprovals 待审批。
5. 收尾给一句下周建议方向。

口径是"近 7 天滚动",行文用"本周";数字缺失就略过那条,不要写"暂无"。
"""

MANAGEMENT_DAILY_SYSTEM = """你是「智巡」管理 AI 的日报撰写助手。基于后端聚合好的"今日"数据,为设施巡检主管写一段「今日一句话结论」,60-110 字,无 markdown、无项目符号,1-2 句话。

核心回答这件事:今天有没有问题、谁去处理。覆盖(只用给的数字,绝不编造):
1. 今日完成巡检数 = overview.recordRecent(不是 execution 的任务数!);是否有异常看 conclusion.hasAbnormal / abnormalCount。
2. 待处理:待复核 + 待审批(conclusion.pendingCount)。
3. 任务执行(execution 指工程/复查任务,不是巡检记录):用 done/plan、overdue 描述任务在途与逾期,措辞用"任务"而非"巡检"。
4. 若 abnormalList 里有异常,点名 1-2 个最该今天处理的(用 point/template + field),并提示责任人(assignee)跟进。
5. 结尾给一句接下来要盯什么。

严格区分"今日巡检数(recordRecent)"与"任务数(execution)",不要把任务数说成巡检数。行文用"今日",语气干练像晨会一句话。某项为 0 或缺失就别提它。
"""


# ===== /analyze =====


def analyze(payload: dict) -> dict:
    api_key = get_api_key()
    template = payload.get("template") or {}
    template_id = template.get("id", "")
    paper_ocr = bool(payload.get("paperOCR"))

    # 模块化提示词:优先用 Go 渲染并随 payload 下发的 promptText;没有则回退读本地 .md
    scenario = (template.get("promptText") or "").strip()
    if not scenario:
        scenario = prompt_for_template(template_id, paper_ocr=paper_ocr)
    if not scenario:
        return manual_required("当前模板暂未启用 AI 识别，请直接人工填写")

    if not api_key:
        return retake_required("AI 服务未配置密钥，请联系管理员或转人工填写", payload)

    images = payload.get("images") or []
    fields = template.get("fields") or []
    field_lines = format_fields(fields)
    today = datetime.now().strftime("%Y-%m-%d")

    user_text = (
        scenario
        + "\n\n字段清单：\n"
        + field_lines
        + f"\n\ncurrent_date: {today}"
        + "\n\n（图片如下，按上传顺序）"
    )

    content: list = [{"type": "text", "text": user_text}]
    # 字段识别要读仪表读数/日期/小字编号,压图可能糊掉细节降准确度,默认全分辨率;
    # 仅在跨区网络慢时由 QWEN_VISION_COMPRESS=1 开启
    compress = os.environ.get("QWEN_VISION_COMPRESS", "0") == "1"
    for image in images[:20]:
        path = image.get("path") or ""
        url = image_to_data_url(path, force_compress=compress)
        if url:
            content.append({"type": "image_url", "image_url": {"url": url}})

    if len(content) == 1:
        return retake_required("没有可读图片", payload)

    model_name = os.environ.get("QWEN_VISION_MODEL", "qwen-vl-plus")
    # qwen3.x 关思考,字段识别不需要链式推理,大幅提速避免超时
    extra = {"enable_thinking": False} if model_name.lower().startswith("qwen3") else None
    started = time.time()
    try:
        raw = call_qwen_chat(
            model=model_name,
            system=COMMON_PROMPT,
            user_content=content,
            api_key=api_key,
            timeout=int(os.environ.get("QWEN_VISION_TIMEOUT", "90") or "90"),
            extra_body=extra,
        )
    except Exception as exc:
        print(f"[analyze] qwen failed: {exc}", file=sys.stderr)
        return retake_required(f"AI 调用失败：{str(exc)[:80]}，请重拍或转人工", payload)

    try:
        parsed = parse_json_response(raw)
    except Exception as exc:
        print(f"[analyze] parse failed: {exc}\nraw: {raw[:300]}", file=sys.stderr)
        return retake_required("AI 输出无法解析为 JSON，请重拍", payload)

    return build_analyze_response(payload, parsed, model_name, int((time.time() - started) * 1000))


def format_fields(fields: list) -> str:
    lines = []
    for f in fields:
        opts = f.get("options") or []
        opts_str = f"  options={json.dumps(opts, ensure_ascii=False)}" if opts else ""
        lines.append(
            f"- code={f.get('code')}  label={f.get('label')}  "
            f"kind={f.get('kind')}  required={f.get('required', False)}{opts_str}"
        )
    return "\n".join(lines)


def build_analyze_response(payload: dict, parsed: dict, model: str, duration_ms: int) -> dict:
    record_id = payload.get("recordId") or f"rec_{uuid.uuid4().hex[:8]}"
    warnings = parsed.get("warnings") or []
    recognized = normalize_recognized_fields(parsed.get("recognizedFields") or [])
    status = parsed.get("recognitionStatus", "recognized" if recognized else "retake_required")
    if recognized and status == "retake_required":
        status = "recognized"
    retake_reason = "" if status == "recognized" else parsed.get("retakeReason", "图片识别不稳定")
    return {
        "schemaVersion": "1.0",
        "analysisId": f"ana_{uuid.uuid4().hex[:10]}",
        "recordId": record_id,
        "model": {
            "provider": "dashscope",
            "name": model,
            "version": "openai-compatible",
        },
        "processedAt": now_iso(),
        "durationMs": duration_ms,
        "recognizedFields": recognized,
        "recognitionStatus": status,
        "retakeReason": retake_reason,
        "observations": parsed.get("observations") or [],
        "warnings": warnings,
    }


ENERGY_READING_CODES = {"z1_reading", "z2_reading", "z3_reading", "z4_reading"}
WATER_READING_CODES = {"living_water_reading", "fire_water_reading"}


def normalize_recognized_fields(fields: list) -> list:
    out = []
    for f in fields:
        if not isinstance(f, dict):
            continue
        code = str(f.get("code", "")).strip()
        value = str(f.get("value", "")).strip()
        if not code or not value:
            continue
        value, normalized_reason = normalize_meter_value(code, value)
        try:
            confidence = float(f.get("confidence", 0.7))
        except (TypeError, ValueError):
            confidence = 0.7
        reason = str(f.get("reason", ""))
        if normalized_reason:
            reason = f"{reason}；{normalized_reason}" if reason else normalized_reason
        out.append({
            "code": code,
            "label": str(f.get("label", "")),
            "value": value,
            "confidence": max(0.0, min(confidence, 1.0)),
            "reason": reason[:80],
        })
    return out


def normalize_meter_value(code: str, value: str) -> tuple[str, str]:
    if code in ENERGY_READING_CODES:
        normalized = normalize_energy_value(value)
        if normalized != value:
            return normalized, "已去除能耗表读数单位"
    if code in WATER_READING_CODES:
        normalized = normalize_mechanical_water_value(value)
        if normalized != value:
            return normalized, "机械水表只取黑色字轮整数位"
    return value, ""


def normalize_energy_value(value: str) -> str:
    raw = value.strip().replace(",", "")
    raw = re.sub(r"(kwh|kw·h|kw|wh|k)$", "", raw, flags=re.I).strip()
    return raw if re.fullmatch(r"\d+(?:\.\d+)?", raw) else value


def normalize_mechanical_water_value(value: str) -> str:
    raw = value.strip().replace(",", "")
    if not re.fullmatch(r"\d+(?:\.\d+)?", raw):
        return value
    if "." not in raw:
        return raw.lstrip("0") or "0"
    left, right = raw.split(".", 1)
    if set(right) <= {"0"}:
        return left.lstrip("0") or "0"
    if len(right) == 1 and len(left) <= 3:
        return (left + right).lstrip("0") or "0"
    return value


def manual_required(reason: str) -> dict:
    return {
        "schemaVersion": "1.0",
        "analysisId": f"ana_{uuid.uuid4().hex[:10]}",
        "model": {"provider": "skip", "name": "manual", "version": "1.0"},
        "processedAt": now_iso(),
        "recognizedFields": [],
        "recognitionStatus": "manual_required",
        "retakeReason": reason,
        "observations": [],
        "warnings": [],
    }


def retake_required(reason: str, payload: dict) -> dict:
    return {
        "schemaVersion": "1.0",
        "analysisId": f"ana_{uuid.uuid4().hex[:10]}",
        "recordId": payload.get("recordId", ""),
        "model": {"provider": "validation", "name": "rule", "version": "1.0"},
        "processedAt": now_iso(),
        "recognizedFields": [],
        "recognitionStatus": "retake_required",
        "retakeReason": reason,
        "observations": [],
        "warnings": [],
    }


# ===== /summarize =====


def summarize(payload: dict) -> dict:
    api_key = get_api_key()
    if not api_key:
        result = fallback_summarize(payload)
        result["model"] = "fallback-no-key"
        return result

    text_model = os.environ.get("QWEN_TEXT_MODEL", "qwen-plus")
    user_text = json.dumps(payload, ensure_ascii=False)
    try:
        raw = call_qwen_chat(
            model=text_model,
            system=SUMMARY_PROMPT,
            user_content=user_text,
            api_key=api_key,
            timeout=10,
            max_retries=1,
        )
    except Exception as exc:
        print(f"[summarize] qwen failed: {exc}", file=sys.stderr)
        result = fallback_summarize(payload)
        result["model"] = "fallback-call-failed"
        return result

    try:
        parsed = parse_json_response(raw)
    except Exception as exc:
        print(f"[summarize] parse failed: {exc}", file=sys.stderr)
        result = fallback_summarize(payload)
        result["model"] = "fallback-parse-failed"
        return result

    abnormal_fields = summarize_abnormal_fields(payload)
    tags = [str(t) for t in (parsed.get("tags") or [])][:5]
    if abnormal_fields and "异常待跟进" not in tags:
        tags = ["异常待跟进"] + [t for t in tags if t != "正常"]
    elif not abnormal_fields and not tags:
        tags = ["正常"]

    return {
        "summary": enforce_summary_policy(str(parsed.get("summary", "")).strip(), payload, abnormal_fields),
        "tags": tags[:5],
        "recommendations": normalize_recommendations(parsed.get("recommendations") or []),
        "model": text_model,
    }


def summary_field_label(field: dict) -> str:
    return str(field.get("label") or field.get("name") or field.get("code") or "").strip()


def summary_field_value(field: dict) -> str:
    return str(field.get("value") or "").strip().strip("。；;,， ")


def summary_is_occurrence_label(label: str) -> bool:
    """发生即异常的问题：答"否"是正常，答"是/有"才异常。
    与 Go inferOverallStatus/isOccurrenceLabel 对齐：不能用裸"报警/卡阻/漏水"匹配，
    否则"报警装置有效=是""无卡阻=是"会被误判为异常。"""
    # "有无X""是否有X" 是问"有没有发生" → occurrence（有/是=异常）
    if any(kw in label for kw in ("有无", "是否有")):
        return True
    # 正向描述（无异响/无卡阻/……完好有效）→ 非 occurrence（是=好）
    if any(kw in label for kw in ("无异", "无报警", "无告警", "无漏", "无故障", "无卡阻", "无渗漏")):
        return False
    return any(kw in label for kw in (
        "异常", "是否漏水", "是否报警", "是否告警", "是否渗漏",
        "存在异响", "存在异味", "存在卡阻", "有异响", "有异味",
    ))


def summary_value_is_abnormal(label: str, value: str) -> bool:
    if not value:
        return False
    normal_phrases = (
        "正常", "完好", "合格", "通过", "有效", "齐全", "无异常", "无报警", "无告警",
        "无故障", "无破损", "无缺失", "无漏水", "无渗漏", "无异响", "无异味",
        "未发现异常", "未发现报警", "未发现故障", "未发现漏水", "未过期",
        "没有异常", "没有报警", "没有故障", "没有问题",
    )
    if any(p in value for p in normal_phrases):
        return False
    if value in ("否", "无"):
        return not summary_is_occurrence_label(label)
    if value in ("是", "有"):
        return summary_is_occurrence_label(label)
    abnormal_phrases = (
        "异常", "故障", "损坏", "报警", "告警", "破损", "裂纹", "缺失", "漏水",
        "渗漏", "异响", "异味", "卡阻", "超限", "过期", "不正常", "不可用",
    )
    return any(p in value for p in abnormal_phrases)


def summarize_abnormal_fields(payload: dict) -> list[str]:
    abnormal: list[str] = []
    for field in payload.get("fields") or []:
        if not isinstance(field, dict):
            continue
        label = summary_field_label(field)
        value = summary_field_value(field)
        if not label:
            continue
        if not value:
            if field.get("required"):
                abnormal.append(f"{label}未填写")
            continue
        if summary_value_is_abnormal(label, value):
            abnormal.append(f"{label}={value}")

    note = str(payload.get("abnormalNote") or "").strip()
    if note and not any(p in note for p in ("无异常", "未发现异常", "没有异常")):
        abnormal.append(f"异常说明={note[:30]}")
    return abnormal


def enforce_summary_policy(summary: str, payload: dict, abnormal_fields: list[str] | None = None) -> str:
    abnormal_fields = abnormal_fields if abnormal_fields is not None else summarize_abnormal_fields(payload)
    summary = re.sub(r"\s+", " ", summary or "").strip()
    if not summary:
        fields = payload.get("fields") or []
        parts = [f"{summary_field_label(f)}={summary_field_value(f)}" for f in fields[:6] if isinstance(f, dict)]
        summary = (
            f"{payload.get('inspector', '巡检员')}在{payload.get('inspectionTime', '')}"
            f"完成{payload.get('templateName', '巡检')}，关键字段：{'; '.join(parts) or '无'}。"
        )

    # 去掉模型/历史可能写的旧式"异常提示：…""低风险总结：…"前后缀（历史提示词遗留，啰嗦）
    summary = re.sub(r"^异常提示[:：][^。；;]*[。；;]?\s*", "", summary).strip()
    summary = re.sub(r"\s*低风险总结[:：].*$", "", summary).strip()
    summary = summary.lstrip("。；; ").strip()
    # 干净拼装：有异常 → 以一句"待跟进 N 项：…"开头，正文跟随；无异常 → 直接用正文
    if abnormal_fields:
        labels = "、".join(abnormal_fields[:4])
        if len(abnormal_fields) > 4:
            labels += "等"
        head = f"待跟进 {len(abnormal_fields)} 项：{labels}。"
        summary = head + summary if summary else head
    elif not summary:
        summary = "本次已确认字段未发现异常。"
    return summary


def normalize_recommendations(raw_list: list) -> list:
    out = []
    for r in (raw_list or [])[:3]:
        if not isinstance(r, dict):
            continue
        priority = r.get("priority", "low")
        if priority not in ("high", "medium", "low"):
            priority = "low"
        out.append({
            "priority": priority,
            "category": str(r.get("category", "下次巡检关注"))[:20],
            "text": str(r.get("text", "")).strip()[:120],
            "basis": str(r.get("basis", "")).strip()[:80],
        })
    return out


CHAT_SYSTEM_PROMPT = """你是「智巡 AI 助手」，服务于设施巡检后台的主管/管理员。
你的工作是：
- 用简洁、专业、口语化的中文回答关于资产、巡检、AI 识别、异常处理、报表的问题
- 主管会问"今天有几个异常"、"AI 准确率怎么样"、"派任务给谁"这类业务问题
- 优先用数据 + 结论 + 建议三段回答；如果数据不足，直说"暂无数据"
- 输出 60-180 字以内，避免长段落；多用「·」分隔
- 不要承诺无法兑现的操作（你只是答疑，无法直接派发任务/审批），但可以告诉用户去哪个菜单操作

context 字段可能包含本平台当前快照（资产数/异常数/记录数等），请尽量引用真实数字。
"""


def chat(payload: dict) -> dict:
    api_key = get_api_key()
    message = (payload.get("message") or "").strip()
    if not message:
        return {"reply": "请输入想问的问题，例如「今天有几条异常」。", "model": "noop"}
    if not api_key:
        return {
            "reply": "未配置 AI 密钥，无法在线对话。请在系统配置中填入 DASHSCOPE_API_KEY。",
            "model": "no-key",
        }
    history = payload.get("history") or []
    context = payload.get("context") or {}
    # 把上下文塞进 user_content（避免污染 system）
    ctx_blob = json.dumps(context, ensure_ascii=False) if context else ""
    user_lines = []
    if ctx_blob:
        user_lines.append(f"[平台数据快照] {ctx_blob}")
    # 简单拼接最近几轮历史
    for turn in history[-4:]:
        role = turn.get("role", "user")
        text = (turn.get("text") or "").strip()
        if not text:
            continue
        prefix = "我" if role == "user" else "AI"
        user_lines.append(f"{prefix}：{text}")
    user_lines.append(f"我：{message}")
    user_lines.append("AI：")
    user_text = "\n".join(user_lines)

    text_model = os.environ.get("QWEN_TEXT_MODEL", "qwen-plus")
    try:
        raw = call_qwen_chat(
            model=text_model,
            system=CHAT_SYSTEM_PROMPT,
            user_content=user_text,
            api_key=api_key,
            timeout=12,
            max_retries=1,
        )
    except Exception as exc:
        print(f"[chat] qwen failed: {exc}", file=sys.stderr)
        return {
            "reply": f"AI 接口暂不可用：{str(exc)[:80]}。请稍后再试。",
            "model": "fallback-call-failed",
        }
    reply = (raw or "").strip()
    # qwen 偶尔会反引号包代码块或 "AI：" 前缀，清掉
    if reply.startswith("AI："):
        reply = reply[3:].strip()
    if reply.startswith("```"):
        reply = reply.strip("`").strip()
    if not reply:
        reply = "AI 没有给出回复，请换种问法再试。"
    return {"reply": reply, "model": text_model}


def fallback_summarize(payload: dict) -> dict:
    fields = payload.get("fields") or []
    parts = [f"{f.get('label')}={f.get('value')}" for f in fields[:6]]
    summary = (
        f"{payload.get('inspector', '巡检员')} 在 {payload.get('inspectionTime', '')} "
        f"完成 {payload.get('templateName', '巡检')}，关键字段：{'; '.join(parts) or '无'}。"
    )
    abnormal_fields = summarize_abnormal_fields(payload)
    return {
        "summary": enforce_summary_policy(summary, payload, abnormal_fields),
        "tags": ["异常待跟进" if abnormal_fields else "正常", "自动摘要降级"],
        "recommendations": [],
        "model": "fallback",
    }


# ===== /classify =====


def classify(payload: dict) -> dict:
    api_key = get_api_key()
    paths = payload.get("imagePaths") or []
    if not paths:
        return {
            "templateId": "unknown",
            "templateName": "无法识别",
            "confidence": 0,
            "reason": "未提供图片",
            "alternatives": [],
            "needsManualPick": True,
        }
    if not api_key:
        return {
            "templateId": "unknown",
            "templateName": "无法识别",
            "confidence": 0,
            "reason": "AI 服务未配置密钥",
            "alternatives": [],
            "needsManualPick": True,
        }

    content: list = [{"type": "text", "text": SCENE_PROMPT}]
    # 多看几张:电梯有机房/无机房只差机房那几张,只看前 3 张会漏掉机房照
    # 压图仅用于缓解跨区(美国→中国)上传延迟,默认关;同区服务器全分辨率更准。
    # 场景分类只看大特征,压了不影响精度;由 QWEN_VISION_COMPRESS 控制
    compress = os.environ.get("QWEN_VISION_COMPRESS", "0") == "1"
    for path in paths[:6]:
        url = image_to_data_url(path, force_compress=compress)
        if url:
            content.append({"type": "image_url", "image_url": {"url": url}})

    if len(content) == 1:
        return {
            "templateId": "unknown",
            "templateName": "无法识别",
            "confidence": 0,
            "reason": "图片读取失败",
            "alternatives": [],
            "needsManualPick": True,
        }

    model = os.environ.get("QWEN_VISION_MODEL", "qwen-vl-plus")
    cls_timeout = int(os.environ.get("QWEN_VISION_TIMEOUT", "90") or "90")
    # qwen3.x 是混合思考模型,关掉思考(enable_thinking=false)大幅提速;分类不需要链式推理
    extra = {"enable_thinking": False} if model.lower().startswith("qwen3") else None
    try:
        raw = call_qwen_chat(
            model=model,
            system="你只输出严格 JSON。",
            user_content=content,
            api_key=api_key,
            timeout=cls_timeout,
            max_retries=1,
            extra_body=extra,
        )
    except Exception as exc:
        print(f"[classify] qwen failed: {exc}", file=sys.stderr)
        return {
            "templateId": "unknown",
            "templateName": "无法识别",
            "confidence": 0,
            "reason": f"AI 调用失败：{str(exc)[:60]}",
            "alternatives": [],
            "needsManualPick": True,
        }

    try:
        parsed = parse_json_response(raw)
    except Exception as exc:
        print(f"[classify] parse failed: {exc}", file=sys.stderr)
        return {
            "templateId": "unknown",
            "templateName": "无法识别",
            "confidence": 0,
            "reason": "AI 输出解析失败",
            "alternatives": [],
            "needsManualPick": True,
        }

    template_id = str(parsed.get("templateId", "unknown")).strip()
    confidence = float(parsed.get("confidence", 0))
    needs_manual = bool(parsed.get("needsManualPick", confidence < 0.7 or template_id == "unknown"))
    return {
        "templateId": template_id,
        "templateName": str(parsed.get("templateName", "")).strip(),
        "confidence": max(0.0, min(confidence, 1.0)),
        "reason": str(parsed.get("reason", ""))[:60],
        "alternatives": parsed.get("alternatives") or [],
        "needsManualPick": needs_manual,
    }


# ===== /management/* (DeepSeek 管理 AI,阶段一只返回 mock) =====
#
# 阶段一目的:把"前端→后端→ai-service→AI"整条链路先打通,DeepSeek 真接通在阶段二。
# 返回结构必须跟阶段二真打 DeepSeek 时一致,这样前端代码不用改两遍。


def management_chat(payload: dict) -> dict:
    """先打真 DeepSeek,失败/没 key 走 rule-based mock 降级。
    后端 risk_score / context 始终可用,即使 AI 挂了主管也能看见东西。
    """
    key = get_deepseek_key()
    model = os.environ.get("DEEPSEEK_CHAT_MODEL", "deepseek-chat")
    if not key:
        return management_chat_mock(payload)
    message = (payload.get("message") or "").strip()
    context = payload.get("context") or {}
    history = payload.get("history") or []
    if not message:
        return {"reply": "请告诉我想问的问题。", "model": "noop", "isMock": False}

    # 拼上下文 + 最近 6 轮历史 + 本轮问题
    parts = []
    if context:
        parts.append(f"[当前看板数据 JSON]\n{json.dumps(context, ensure_ascii=False)}")
    for turn in history[-6:]:
        role = "管理员" if turn.get("role") == "user" else "AI"
        text = (turn.get("text") or "").strip()
        if text:
            parts.append(f"{role}: {text}")
    parts.append(f"管理员: {message}")
    parts.append("AI:")
    user_text = "\n\n".join(parts)

    timeout = int(os.environ.get("DEEPSEEK_TIMEOUT_SECONDS", "30") or "30")
    try:
        reply, actual_model = call_deepseek_chat(
            model=model, system=MANAGEMENT_CHAT_SYSTEM,
            user_content=user_text, api_key=key, timeout=timeout,
        )
    except Exception as exc:
        print(f"[management/chat] deepseek failed, fallback mock: {exc}", file=sys.stderr)
        out = management_chat_mock(payload)
        out["fallbackReason"] = str(exc)[:120]
        return out
    return {
        "reply": (reply or "").strip(),
        "model": actual_model,
        "generatedAt": now_iso(),
        "evidence": [],
        "isMock": False,
    }


def management_chat_mock(payload: dict) -> dict:
    """rule-based 兜底:reply 内容用真实数据组装,不暴露 [mock] 字样。
    isMock 标记交给前端,由前端决定 model 标签是否打"预览模式"角标。
    """
    context = payload.get("context") or {}
    overview = context.get("overview") or {}
    attention = context.get("attention") or []
    reply_lines = []
    top = (attention[0] if isinstance(attention, list) and attention else {}) or {}
    if top:
        level = {"danger": "高风险", "warning": "需关注", "normal": "可观察"}.get(top.get("riskLevel"), "需关注")
        # 重点前置:一句加粗,作为唯一视觉焦点
        reply_lines.append(
            "首要关注 **{name}**(风险 {score} · {level})".format(
                name=top.get("assetName", "-"),
                score=top.get("riskScore", "-"),
                level=level,
            )
        )
        # 依据:最多 2 条短句,分行、去掉"建议/动作"这类导航文字
        for raw in (top.get("reasons") or [])[:2]:
            r = str(raw).strip().rstrip("。;;,，")
            if r:
                reply_lines.append("· " + r)
    if overview:
        # 末尾一行轻量台账规模,不与上面的异常次数混淆
        reply_lines.append(
            "台账规模:在册 {at} 台 · 本期巡检 {rr} 条 · 待审批 {pa}".format(
                at=overview.get("assetTotal", "-"),
                rr=overview.get("recordRecent", "-"),
                pa=overview.get("pendingApprovals", "-"),
            )
        )
    if not reply_lines:
        reply_lines.append("当前数据较少,等下次提交后再问我会更准。")
    return {
        "reply": "\n".join(reply_lines),
        "model": "deepseek-v4-flash",
        "generatedAt": now_iso(),
        "evidence": [],
        "isMock": True,
    }


def management_analyze(payload: dict) -> dict:
    """先打真 DeepSeek(用 report model),失败 fallback mock。kind=weekly 用周报 prompt。"""
    key = get_deepseek_key()
    model = os.environ.get("DEEPSEEK_REPORT_MODEL", "deepseek-chat")
    kind = payload.get("kind")
    system = MANAGEMENT_WEEKLY_SYSTEM if kind == "weekly" else MANAGEMENT_DAILY_SYSTEM if kind == "daily" else MANAGEMENT_REPORT_SYSTEM
    if not key:
        return management_analyze_mock(payload)
    user_text = json.dumps(payload, ensure_ascii=False)
    timeout = int(os.environ.get("DEEPSEEK_TIMEOUT_SECONDS", "30") or "30")
    try:
        reply, actual_model = call_deepseek_chat(
            model=model, system=system,
            user_content=user_text, api_key=key, timeout=timeout,
        )
    except Exception as exc:
        print(f"[management/analyze] deepseek failed, fallback mock: {exc}", file=sys.stderr)
        out = management_analyze_mock(payload)
        out["fallbackReason"] = str(exc)[:120]
        return out
    return {
        "summary": (reply or "").strip(),
        "attention": (payload.get("attention") or [])[:5],
        "recommendations": [],
        "model": actual_model,
        "generatedAt": now_iso(),
        "isMock": False,
    }


def management_analyze_mock(payload: dict) -> dict:
    overview = (payload.get("overview") or {})
    attention = (payload.get("attention") or [])[:5]
    summary_parts = []
    if overview:
        summary_parts.append(
            "本期共 {at} 台资产、{rr} 条巡检,正常 {an} / 待复核 {aw} / 异常 {ad}".format(
                at=overview.get("assetTotal", 0),
                rr=overview.get("recordRecent", 0),
                an=overview.get("assetNormal", 0),
                aw=overview.get("assetWarning", 0),
                ad=overview.get("assetDanger", 0),
            )
        )
    if attention:
        names = "、".join(a.get("assetName", "-") for a in attention[:3])
        summary_parts.append(f"重点关注 {names}")
    summary = "。".join(summary_parts) + "。" if summary_parts else "暂无足够数据生成摘要。"
    return {
        "summary": summary,
        "attention": attention,
        "recommendations": [],
        "model": "deepseek-v4-pro",
        "generatedAt": now_iso(),
        "isMock": True,
    }


# ===== HTTP server =====


class Handler(BaseHTTPRequestHandler):
    server_version = "InspectAIService/0.2"

    def log_message(self, fmt, *args):
        sys.stderr.write(f"{self.address_string()} - {fmt % args}\n")

    def do_GET(self):
        if self.path == "/health":
            has_dashscope_key = bool(get_api_key())
            has_deepseek_key = bool(get_deepseek_key())
            degraded_reasons = []
            if not has_dashscope_key:
                degraded_reasons.append("dashscope_key_missing")
            if not has_deepseek_key:
                degraded_reasons.append("deepseek_key_missing")
            write_json(self, 200, {
                "status": "ok",
                "service": "ai-service",
                # Keep hasKey for compatibility with the existing local checks.
                "hasKey": has_dashscope_key,
                "hasDashscopeKey": has_dashscope_key,
                "hasVisionKey": has_dashscope_key,
                "hasDeepSeekKey": has_deepseek_key,
                "managementAIReady": has_deepseek_key,
                "managementAI": "deepseek" if has_deepseek_key else "rule_fallback",
                "degradedReason": ",".join(degraded_reasons),
                "promptsLoaded": len(TEMPLATE_PROMPT_MAP) + 3,  # +common+scene+summary
            })
            return
        write_json(self, 404, {"error": "not_found"})

    def do_POST(self):
        try:
            payload = read_json(self)
            if self.path == "/analyze":
                write_json(self, 200, analyze(payload))
            elif self.path == "/summarize":
                write_json(self, 200, summarize(payload))
            elif self.path == "/classify":
                write_json(self, 200, classify(payload))
            elif self.path == "/chat":
                write_json(self, 200, chat(payload))
            elif self.path == "/management/chat":
                write_json(self, 200, management_chat(payload))
            elif self.path == "/management/analyze":
                write_json(self, 200, management_analyze(payload))
            else:
                write_json(self, 404, {"error": "not_found"})
        except Exception as exc:
            print(f"[handler] {self.path} error: {exc}", file=sys.stderr)
            import traceback
            traceback.print_exc()
            write_json(self, 500, {"error": "internal", "message": str(exc)[:200]})


if __name__ == "__main__":
    host = os.environ.get("AI_SERVICE_ADDR", "127.0.0.1")
    port = int(os.environ.get("AI_SERVICE_PORT", "19100"))
    has_key = bool(get_api_key())
    print(f"AI service listening on http://{host}:{port}")
    print(f"  Has API key: {has_key}")
    print(f"  Prompts dir: {PROMPTS_DIR}")
    httpd = ThreadingHTTPServer((host, port), Handler)
    httpd.serve_forever()
