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
    "elevator_no_room": "elevator_visual",
    "elevator_machine_room": "elevator_visual",
    "escalator": "elevator_visual",
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


def image_to_data_url(path: str) -> str:
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
    if len(data) > 8 * 1024 * 1024:
        # 大于 8MB 不压缩也接得住但偏大；尝试用 PIL 压一下
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


# ===== /analyze =====


def analyze(payload: dict) -> dict:
    api_key = get_api_key()
    template = payload.get("template") or {}
    template_id = template.get("id", "")
    paper_ocr = bool(payload.get("paperOCR"))

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
    for image in images[:6]:
        path = image.get("path") or ""
        url = image_to_data_url(path)
        if url:
            content.append({"type": "image_url", "image_url": {"url": url}})

    if len(content) == 1:
        return retake_required("没有可读图片", payload)

    model_name = os.environ.get("QWEN_VISION_MODEL", "qwen-vl-plus")
    started = time.time()
    try:
        raw = call_qwen_chat(
            model=model_name,
            system=COMMON_PROMPT,
            user_content=content,
            api_key=api_key,
            timeout=60,
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

    return {
        "summary": str(parsed.get("summary", "")).strip(),
        "tags": [str(t) for t in (parsed.get("tags") or [])][:5],
        "recommendations": normalize_recommendations(parsed.get("recommendations") or []),
        "model": text_model,
    }


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
    return {
        "summary": summary,
        "tags": ["自动摘要降级"],
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
    for path in paths[:3]:
        url = image_to_data_url(path)
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
    try:
        raw = call_qwen_chat(
            model=model,
            system="你只输出严格 JSON。",
            user_content=content,
            api_key=api_key,
            timeout=30,
            max_retries=1,
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


def management_chat_mock(payload: dict) -> dict:
    message = (payload.get("message") or "").strip()
    context = payload.get("context") or {}
    overview = context.get("overview") or {}
    attention = context.get("attention") or []
    reply_lines = ["[mock] 这是管理 AI 占位回答(阶段一,阶段二接 DeepSeek-V4)。"]
    if message:
        reply_lines.append(f"你问的是:{message}")
    if overview:
        reply_lines.append(
            "看板态势:资产 {at} / 异常 {ad} / 待复核 {aw} / 本期巡检 {rr}".format(
                at=overview.get("assetTotal", "-"),
                ad=overview.get("assetDanger", "-"),
                aw=overview.get("assetWarning", "-"),
                rr=overview.get("recordRecent", "-"),
            )
        )
    if attention:
        top = attention[0] if isinstance(attention, list) and attention else {}
        if top:
            reply_lines.append(
                "重点关注:{name}({score} 分),原因:{reason}".format(
                    name=top.get("assetName", "-"),
                    score=top.get("riskScore", "-"),
                    reason=(top.get("reasons") or ["-"])[0],
                )
            )
    return {
        "reply": "\n".join(reply_lines),
        "model": "mock-v4-flash",
        "generatedAt": now_iso(),
        "evidence": [],
        "mock": True,
    }


def management_analyze_mock(payload: dict) -> dict:
    overview = (payload.get("overview") or {})
    attention = (payload.get("attention") or [])[:5]
    summary_parts = []
    if overview:
        summary_parts.append(
            "本期共 {at} 台资产,完成 {rr} 次巡检,正常 {an} / 待复核 {aw} / 异常 {ad}".format(
                at=overview.get("assetTotal", 0),
                rr=overview.get("recordRecent", 0),
                an=overview.get("assetNormal", 0),
                aw=overview.get("assetWarning", 0),
                ad=overview.get("assetDanger", 0),
            )
        )
    if attention:
        names = ", ".join(a.get("assetName", "-") for a in attention[:3])
        summary_parts.append(f"重点关注:{names}")
    summary = "。".join(summary_parts) + "。" if summary_parts else "暂无足够数据生成摘要。"
    return {
        "summary": "[mock] " + summary,
        "attention": attention,
        "recommendations": [],
        "model": "mock-v4-pro",
        "generatedAt": now_iso(),
        "mock": True,
    }


# ===== HTTP server =====


class Handler(BaseHTTPRequestHandler):
    server_version = "InspectAIService/0.2"

    def log_message(self, fmt, *args):
        sys.stderr.write(f"{self.address_string()} - {fmt % args}\n")

    def do_GET(self):
        if self.path == "/health":
            write_json(self, 200, {
                "status": "ok",
                "service": "ai-service",
                "hasKey": bool(get_api_key()),
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
                write_json(self, 200, management_chat_mock(payload))
            elif self.path == "/management/analyze":
                write_json(self, 200, management_analyze_mock(payload))
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
