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
MANAGEMENT_CHAT_SYSTEM = """你是「智巡」管理后台的 AI 分析助手,服务于设施巡检主管。

工作范围:
- 解读后端给你的台账聚合数据(资产/巡检/异常/复核留痕等)
- 用主管能读懂的语言总结风险、变化、需关注的资产
- 给出可执行的下一步建议(去看哪台资产/补什么巡检/找谁复核)

约束:
- 只回答与本巡检系统数据相关的问题,无关问题直接说"请问与巡检台账相关的问题"
- 严禁编造数据。回答只能基于上下文里给的数字/名称
- 不要假装能修改/审批/派单——你只能告诉用户去后台哪个菜单做
- 回答 80-200 字,简洁专业,优先三段式:结论 + 依据 + 建议动作
- 提到具体资产/记录时,优先用 context 中的真实名称和 ID
"""

MANAGEMENT_REPORT_SYSTEM = """你是「智巡」管理 AI 的报告生成器。基于后端聚合好的数据,
为主管写一段「全局态势摘要」,80-180 字,无 markdown,无项目符号,流水句。

包含:
1. 本期巡检规模与异常情况(资产数/巡检数/正常/待复核/异常)
2. 重点关注的 1-3 个资产(从 attention 列表里挑风险最高的)
3. 一个建议性的方向(下次优先关注什么)

不要编造名称或数字;只用 overview/attention 里有的数据。
"""


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
    for image in images[:20]:
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
    message = (payload.get("message") or "").strip()
    context = payload.get("context") or {}
    overview = context.get("overview") or {}
    attention = context.get("attention") or []
    reply_lines = []
    if message:
        reply_lines.append(f"针对「{message}」,基于近期台账给你一个分析:")
    if overview:
        reply_lines.append(
            "整体态势:在册资产 {at} 台、本期巡检 {rr} 条,异常 {ad} 项 / 待复核 {aw} 项 / 待审批 {pa} 项。".format(
                at=overview.get("assetTotal", "-"),
                ad=overview.get("assetDanger", "-"),
                aw=overview.get("assetWarning", "-"),
                rr=overview.get("recordRecent", "-"),
                pa=overview.get("pendingApprovals", "-"),
            )
        )
    if attention:
        top = attention[0] if isinstance(attention, list) and attention else {}
        if top:
            reasons = top.get("reasons") or []
            reason_text = ";".join(reasons[:2]) if reasons else "—"
            reply_lines.append(
                "首要关注:{name}(风险分 {score},{level}),依据:{reason}。建议:{action}".format(
                    name=top.get("assetName", "-"),
                    score=top.get("riskScore", "-"),
                    level={"danger": "高风险", "warning": "需关注", "normal": "可观察"}.get(top.get("riskLevel"), "需关注"),
                    reason=reason_text,
                    action=top.get("action", "下次巡检重点关注"),
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
    """先打真 DeepSeek(用 report model),失败 fallback mock。"""
    key = get_deepseek_key()
    model = os.environ.get("DEEPSEEK_REPORT_MODEL", "deepseek-chat")
    if not key:
        return management_analyze_mock(payload)
    user_text = json.dumps(payload, ensure_ascii=False)
    timeout = int(os.environ.get("DEEPSEEK_TIMEOUT_SECONDS", "30") or "30")
    try:
        reply, actual_model = call_deepseek_chat(
            model=model, system=MANAGEMENT_REPORT_SYSTEM,
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
