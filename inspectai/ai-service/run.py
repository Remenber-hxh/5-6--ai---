"""InspectAI Assistant - Python AI 微服务

负责：
  POST /classify   - 场景分类（拍照后反推模板）
  POST /analyze    - 字段识别（按模板加载对应 prompt）
  POST /summarize  - 总结生成（事实总结 + 行动建议）
  POST /management/chat, /management/chat-tools, /management/analyze - 管理 AI(DeepSeek)
  POST /prompt/draft-fields - 需求文字 → 字段表
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
from urllib.parse import parse_qs, urlparse

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


# 账号级错误:重试没用、换张图也没用、所有人都会撞上。
# 必须和"这张图识别不出来"区分开 —— 后者是业务常态(照片糊了、角度不对),
# 前者是服务整体不可用,得让管理员立刻知道,而不是让每个巡检员各撞一次。
#
# 取值来自阿里云百炼的错误码文档:
# https://help.aliyun.com/zh/model-studio/error-code
ACCOUNT_ERROR_CODES = (
    "Arrearage",              # 欠费 / 免费额度耗尽
    "InvalidApiKey",          # key 失效或写错
    "Forbidden.Unpurchased",  # 模型未开通
    "AllocationQuota",        # 配额分配用尽
    "Throttling.RateQuota",   # 限流配额用尽
)


# 给最终用户看的话。不写"Arrearage"这种码 —— 巡检员看不懂,
# 也不该让他以为是自己操作错了。
ACCOUNT_ERROR_HINT = "AI 服务暂时不可用(账号额度已用尽),请联系管理员;本次可手动填写"


# 最后一次账号级错误,给 /health 用。运维不该为了确认"是不是欠费"去翻日志。
# 进程内变量:重启即清空 —— 这正是想要的,重启后第一次调用会重新暴露真实状态。
LAST_ACCOUNT_ERROR: dict = {}


def account_error_of(exc: Exception) -> str:
    """从异常里认出账号级错误,返回错误码;不是账号问题则返回空串。"""
    text = str(exc)
    for code in ACCOUNT_ERROR_CODES:
        if code in text:
            LAST_ACCOUNT_ERROR.clear()
            LAST_ACCOUNT_ERROR.update({"code": code, "at": now_iso(), "detail": text[:200]})
            return code
    return ""


# 管理 AI(DeepSeek)的账号级错误。
#
# 【为什么要单独一份】上面那张表全是阿里云的码(Arrearage 等),而 DeepSeek
# 返回的是 "Insufficient Balance" 这类文案 —— 一个都对不上。于是出现过:
# DeepSeek 欠费,管理问答每一句都是兜底文案,而 /health 一片正常,
# 系统页全绿,聊天窗口不吭声,只能靠"回答看着不太对"去猜。
#
# 【也不能和视觉合成一个】视觉走 DashScope、问答走 DeepSeek,是两个账户。
# 合成一个标记的话,DeepSeek 欠费会把"拍照识别还能用"这条关键信息一起抹掉。
CHAT_ACCOUNT_ERROR_MARKERS = (
    ("insufficient balance", "InsufficientBalance"),   # 余额耗尽
    ("insufficient_balance", "InsufficientBalance"),
    ("authentication fails", "InvalidApiKey"),         # key 失效或写错
    ("invalid_api_key", "InvalidApiKey"),
    ("invalid api key", "InvalidApiKey"),
    ("quota", "QuotaExhausted"),                       # 配额用尽
    ("rate_limit_reached", "RateLimited"),
)

LAST_CHAT_ERROR: dict = {}


def chat_account_error_of(exc: Exception) -> str:
    """从 DeepSeek 异常里认出账号级错误。不是账号问题返回空串。"""
    text = str(exc).lower()
    for marker, code in CHAT_ACCOUNT_ERROR_MARKERS:
        if marker in text:
            LAST_CHAT_ERROR.clear()
            LAST_CHAT_ERROR.update({"code": code, "at": now_iso(), "detail": str(exc)[:200]})
            return code
    return ""


# ===== 模型调用的公共层 =====
#
# 【为什么三个调用共用一层】原来 call_qwen_chat / call_deepseek_chat /
# call_deepseek_tools 各写了一遍 curl,三份的超时、重试、错误处理各不相同。
# 并到这一处之后,下面三件事只做一遍:
#
# 1. 【密钥不进命令行】原来是 -H "Authorization: Bearer <key>" 直接放在参数里,
#    同一台机器上 ps 就能看到。现在请求头写进临时文件,curl 用 -H @文件 读,用完即删。
# 2. 【守住调用方给的时间预算】Go 那边等多久是有数的。这边原来按自己的超时
#    再乘重试次数,常常 Go 已经放弃了,这边还在调模型、还在花钱,结果没人收。
#    现在每次尝试前先看还剩多少时间,不够就不发、不重试。
# 3. 【每次调用留一行记录】用途、模型、耗时、成败、token 数。原来只有出错时
#    打一句,正常调用花了多少时间和钱无从查起。

AI_CALL_MIN_SECONDS = 3  # 预算只剩这么点就别发了 —— 发出去也等不到回复


def budget_deadline(payload: dict, default_seconds: float) -> float:
    """调用方给的总时间预算(budgetSeconds) → 截止时刻。没给就用这个接口的默认值。"""
    try:
        seconds = float(payload.get("budgetSeconds") or 0)
    except (TypeError, ValueError):
        seconds = 0
    if seconds <= 0:
        seconds = default_seconds
    return time.time() + seconds


def seconds_left(deadline: float | None) -> float | None:
    return None if deadline is None else deadline - time.time()


class AICallError(RuntimeError):
    """一次模型调用失败。retryable = 换一次可能就好(网络抖动、超时、网关 5xx)。"""

    def __init__(self, message: str, retryable: bool = False):
        super().__init__(message)
        self.retryable = retryable


def log_ai_call(provider: str, model: str, purpose: str, started: float, ok: bool,
                usage: dict | None = None, error: str = "") -> None:
    rec = {
        "provider": provider, "model": model, "purpose": purpose or "-",
        "ms": int((time.time() - started) * 1000), "ok": ok,
    }
    if isinstance(usage, dict):
        rec["promptTokens"] = usage.get("prompt_tokens")
        rec["completionTokens"] = usage.get("completion_tokens")
    if error:
        rec["error"] = error[:160]
    print("[ai-call] " + json.dumps(rec, ensure_ascii=False), file=sys.stderr)


def curl_args(url: str, header_file: str, body_file: str, timeout: int) -> list:
    """curl 的参数。单独拿出来,是为了测试能断言"密钥不在参数里"。"""
    return [
        CURL_PATH,
        "-4",              # 强制 IPv4
        "-s", "-S",        # 静默,但错误照打
        "--noproxy", "*",  # 绕开系统代理(Clash/Fiddler 等会拦 dashscope)
        "-m", str(timeout),
        "-X", "POST", url,
        "-H", f"@{header_file}",
        "--data-binary", f"@{body_file}",
    ]


def _curl_post_once(url: str, body_bytes: bytes, api_key: str, timeout: float) -> dict:
    """发一次 POST,返回解析后的 JSON。网络失败/超时/非 JSON 抛 retryable 的 AICallError。"""
    files = []
    try:
        body = tempfile.NamedTemporaryFile("wb", suffix=".json", delete=False)
        files.append(body.name)
        body.write(body_bytes)
        body.close()
        # NamedTemporaryFile 在 Linux 上建出来就是 0600,只有本进程的用户能读
        head = tempfile.NamedTemporaryFile("w", suffix=".hdr", delete=False, encoding="utf-8")
        files.append(head.name)
        head.write(f"Authorization: Bearer {api_key}\nContent-Type: application/json\n")
        head.close()
        t = max(1, int(timeout))
        try:
            proc = subprocess.run(
                curl_args(url, head.name, body.name, t),
                capture_output=True,
                timeout=t + 5,
            )
        except subprocess.TimeoutExpired:
            raise AICallError(f"timeout after {t}s", retryable=True)
        if proc.returncode != 0:
            err = proc.stderr.decode("utf-8", errors="replace")[:200]
            raise AICallError(f"curl exit {proc.returncode}: {err}", retryable=True)
        raw = proc.stdout.decode("utf-8", errors="replace")
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            # 网关 5xx 常常回一页 HTML —— 换一次通常就好
            raise AICallError(f"response is not JSON: {raw[:120]}", retryable=True)
    finally:
        for f in files:
            try:
                os.unlink(f)
            except OSError:
                pass


def post_chat_completion(*, provider: str, url: str, body: dict, api_key: str,
                         timeout: float, max_retries: int = 0,
                         deadline: float | None = None, purpose: str = "") -> dict:
    """一次 chat/completions 调用(带重试和时间预算),返回响应 JSON(已确认没有 error)。

    【业务错误不重试】欠费、key 失效这类,重试多少次都一样,只会让用户白等。
    """
    body_bytes = json.dumps(body, ensure_ascii=False).encode("utf-8")
    model = str(body.get("model") or "")
    last: Exception | None = None
    for attempt in range(max_retries + 1):
        left = seconds_left(deadline)
        if left is not None and left < AI_CALL_MIN_SECONDS:
            if last:
                raise last
            raise AICallError(f"{provider} 时间预算已用完,未发出请求")
        this_timeout = timeout if left is None else min(timeout, left - 1)
        started = time.time()
        try:
            payload = _curl_post_once(url, body_bytes, api_key, this_timeout)
        except AICallError as exc:
            log_ai_call(provider, model, purpose, started, False, error=str(exc))
            last = exc
            if exc.retryable and attempt < max_retries:
                pause = 0.5 * (2 ** attempt)
                left = seconds_left(deadline)
                if left is None or left > pause + AI_CALL_MIN_SECONDS:
                    time.sleep(pause)
                continue
            raise
        err = payload.get("error")
        if err:
            err_obj = err if isinstance(err, dict) else {"message": str(err)}
            code = err_obj.get("code") or err_obj.get("type", "error")
            msg = f"{provider} error [{code}]: {err_obj.get('message', 'unknown')}"
            log_ai_call(provider, model, purpose, started, False, error=msg)
            raise AICallError(msg)
        if not (payload.get("choices") or []):
            log_ai_call(provider, model, purpose, started, False, error="no choices")
            raise AICallError(f"{provider} response has no choices")
        log_ai_call(provider, model, purpose, started, True, usage=payload.get("usage"))
        return payload
    if last:
        raise last
    raise AICallError(f"{provider} call failed")


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
    deadline: float | None = None,
    purpose: str = "",
) -> str:
    base_url = os.environ.get(
        "DASHSCOPE_BASE_URL",
        "https://dashscope.aliyuncs.com/compatible-mode/v1",
    ).rstrip("/")
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
    payload = post_chat_completion(
        provider="qwen", url=f"{base_url}/chat/completions", body=body, api_key=api_key,
        timeout=timeout, max_retries=max_retries, deadline=deadline, purpose=purpose,
    )
    # 【调用成功就清掉账号故障标记】否则充值/换 key 之后 /health
    # 会一直报 account_Arrearage 直到重启服务 —— 那是假警报,
    # 比不报警更糟:运维会学会忽略它。
    LAST_ACCOUNT_ERROR.clear()
    return (payload["choices"][0].get("message") or {}).get("content") or ""


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
    deadline: float | None = None,
    purpose: str = "",
) -> tuple[str, str]:
    """打 DeepSeek OpenAI 兼容端点;返回 (reply_text, actual_model)。"""
    base_url = os.environ.get("DEEPSEEK_BASE_URL", "https://api.deepseek.com").rstrip("/")
    body = {
        "model": model,
        "messages": [
            {"role": "system", "content": system},
            {"role": "user", "content": user_content},
        ],
        "temperature": temperature,
    }
    payload = post_chat_completion(
        provider="deepseek", url=f"{base_url}/chat/completions", body=body, api_key=api_key,
        timeout=timeout, max_retries=max_retries, deadline=deadline, purpose=purpose,
    )
    content = (payload["choices"][0].get("message") or {}).get("content") or ""
    # 【成功就清掉账号故障标记】否则充值之后 /health 会一直报欠费
    # 直到重启服务 —— 假警报比不报警更糟,人会学会忽略它。
    LAST_CHAT_ERROR.clear()
    return content, payload.get("model") or model


# 管理 AI 的系统 prompt — 阶段一边界约束:只回答台账问题,不修改任何数据
# 管理聊天备用路(不带工具)的提示词 = 下面这段它自己特有的说明 + 后端发来的共用规矩。
#
# 【共用规矩只在后端写一份】数字不能编、不许出现字段名和 id、输出格式、动作提议 ——
# 原来这里也抄了一份,两边写着写着就不一样了:一键派单的规则只加进了其中一份,
# 字数要求一边 50-120、一边 50-90。现在由后端随请求发来(payload.sharedRules,
# 见 go-backend agent_tools.go 的 managementChatSharedRules),这里只讲看板数据怎么读。
MANAGEMENT_CHAT_CONTEXT = """你是「智巡」管理后台的 AI 助手,服务于设施巡检主管,能就「智巡」全系统答疑——
巡检计划、巡检记录、资产台账、审批中心、设备健康、数据看板、用户与权限、操作日志、系统管理、复核质量等所有维度都答。

上下文 JSON 给了其中几类的实时数据,各字段含义:
- overview:整体规模与异常/待复核/待审批计数
- topRiskAssets:风险最高的资产(风险分、异常次数、原因)。风险分是**百分制(0-100)**:≥70 高风险、40-69 需关注;提到分数时可写"85 分(满分 100)"
- repeatedIssues:重复出现的异常字段(哪台设备哪个字段反复异常、次数)
- pendingReviews:待复核记录(needsReview)+ 待审批申请(pendingApprovals)
- inspectorQuality:各巡检员质量(没看图就确认 noPhotoConfirm、人工修正等)
- numericDrift:数值字段漂移明细(字段在两次巡检间的变化率)
- planTasks:巡检/工程任务概览(total 总数、done 已完成、processing 进行中、notStarted 待执行、overdue 逾期,openItems 给了前几条未完成任务的名称/负责人/截止时间)

**先读懂管理员到底问什么,再答:**
- 问"复核率 / 待复核 / 谁没看图就确认" → 用 pendingReviews 和 inspectorQuality 作答
- 问"重复风险 / 反复异常 / 某类设备(如无机房电梯)" → 用 repeatedIssues 作答
- 问"字段漂移 / 数值变化 / 趋势" → 用 numericDrift 作答
- 问宽泛的"重点关注 / 今天优先处理什么" → 以 topRiskAssets 第一名作答
- 问"巡检计划 / 本周任务 / 排班 / 任务进展" → 用 planTasks 作答:说总数、几条完成/进行中/待执行/逾期,并从 openItems 点名 1-2 条未完成任务(名称+负责人+截止)。planTasks 里有数据就直接用,别说"数据暂未提供"
- 问单台设备的逐次历史、单条记录、单个人的明细("K07 上个月异常几次""8月3号那次谁巡的")→ 这些上下文里都没有,按下面的规矩如实说并指路,不估算
- **绝不要每个问题都把最高风险那台资产复述一遍**;要紧扣问的那件事。

没数据时怎么指路(照此风格):
  · 问"这台设备的逐次记录" → "这台设备的逐次记录我这儿看不到,在「设备健康」里点进 K07 就有完整历史。"
  · 问"操作日志谁改了数据" → "操作日志会留痕每位成员的改动与时间,我这边暂无实时日志明细,可在操作日志里按人或时间筛选查看。"
"""

# 后端没发共用规矩时的保底(比如单独升级了 AI 服务):只留最要紧的两句,不是第二份完整规则。
MANAGEMENT_CHAT_RULES_FALLBACK = """【数字不能编】具体的数字、次数、日期、人名、设备编号只能来自给你的数据,没有就如实说并指路,不估算。
【不许出现内部字段名和 id】不写英文字段名、完整设备 id、模板代号,说人话。"""


def management_chat_system(payload: dict) -> str:
    rules = str(payload.get("sharedRules") or "").strip()
    if not rules:
        print("[management/chat] 后端没发 sharedRules,用保底规矩", file=sys.stderr)
        rules = MANAGEMENT_CHAT_RULES_FALLBACK
    return MANAGEMENT_CHAT_CONTEXT + "\n" + rules


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

硬性要求:
- **异常和风险写在最前面**,开头直接点本周最该关注的设备/问题;**绝不要用"本周巡检规模大幅上升"这类套话开场**。
- **绝不能把英文字段名搬进文字**(overview/topRiskAssets/riskLevel/danger 等一律不准出现);风险等级用中文——danger=高风险、warning=需关注、normal=正常;说"风险最高的设备"而不是"topRiskAssets"。
- 设备、人员、异常数量只能用给的数据,绝不编造。
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


# ===== 读数复核:把读数区裁出来放大,再读一遍 =====
#
# 【为什么要有这一遍】拿 2026-09-10 那条紫菡记录的真照片实测过:
# 生活水表真值 1992,整张图送进去连着三轮都读成 1998,而且每次都给 0.95 ——
# 不是"有时候错",是稳定地错、还稳定地自信。这种错没法靠重试或看置信度发现。
# 同一块表裁出字轮窗口放大后再问,三轮全对。
#
# 原因很直白:1200x1600 的现场照里,水表字轮窗口只占 110x30 像素,
# 黑底白字还压过一道 JPEG。模型看的和人眯着眼看是一样的。
#
# 【为什么不是一开始就裁】裁之前得先知道往哪儿裁。所以第一遍照常看整图,
# 顺便让它报出读数区的框(_common.md 里那条规则),第二遍只看框里那一小块。
# 两次调用,第二次送的图很小,token 反而比第一次省。
#
# 【两次不一致时采信放大那次】它看到的细节更多 —— 这是实测结论,不是猜的。
# 但一定同时压低 confidence 触发人工复核:采信不等于确信。

SECOND_LOOK_SYSTEM = """你是工程表计读数的复核员。

输入是【已经裁剪并放大过的读数区特写】,每张图对应一个字段。
你的唯一任务:把每张图里的**完整读数**读出来。

严格输出 JSON,不要 markdown、不要解释:

{"readings": [{"code": "字段编码", "value": "读到的数字", "confidence": 0.0, "note": "不超过10字"}]}

规则:
1. **只读你确实看清的**。看不清就不要放进 readings —— 漏一个比编一个好。
2. **读的是整个读数,不是其中一行**。LCD 常把一个读数分成上下两行显示,
   两行合起来才是一个数;只读其中一行就是错的。下面的场景规则会说清怎么合。
3. **不要凭空插入小数点**。小数点只能来自屏幕上真实可见的小数点,
   或来自下面场景规则里明确给出的固定表型格式;换行位置不是小数点。
4. **机械水表黑色字轮窗口里的数字全是整数位**,包括最后一位;红色字轮才是小数,忽略。
   前导 0 去掉(000722 读作 722)。
5. 不要输出单位、设备编号、二维码、型号、时间戳。
6. confidence 按字符可读性给:笔画完整清晰 >0.9;有反光/毛边/可能混淆(B与8、6与8、2与8)给 0.6-0.85。

**这一遍不需要你判断哪张图是哪块表** —— 每张图对应哪个字段,上面已经写明了。
你只管把数字读准、读全。
"""

# 【场景规则必须跟着进第二遍】第一遍之所以能把 6019/7924 读成 60197.924,
# 靠的就是场景 prompt 里那条"5 位整数 + 3 位小数"。第二遍只给一句
# "把数字读出来"的话,模型看着放得很清楚的屏,照样只读下半行 7924 ——
# 实测就是这么翻车的:裁剪把水表从 1998 救成 1992,却把三块电表全读成了下半行。
SECOND_LOOK_SCENE_HEADER = "\n\n## 本场景的读数规则(和第一遍同一份,必须遵守)\n\n"


def _valid_bbox(box) -> bool:
    if not isinstance(box, (list, tuple)) or len(box) != 4:
        return False
    try:
        x0, y0, x1, y1 = (float(v) for v in box)
    except (TypeError, ValueError):
        return False
    if not all(-0.05 <= v <= 1.05 for v in (x0, y0, x1, y1)):
        return False
    # 太小(基本是个点)或几乎整张图(等于没裁)都没有复核价值
    w, h = abs(x1 - x0), abs(y1 - y0)
    return 0.005 < w <= 1.0 and 0.005 < h <= 1.0 and w * h < 0.85


# 复核裁剪最少往外放多少(占整张图的比例),见 crop_reading_area
SECOND_LOOK_MIN_PAD_X = 0.06
SECOND_LOOK_MIN_PAD_Y = 0.08


def crop_reading_area(path: str, box, pad: float = 0.25, target_edge: int = 1400):
    """按归一化 bbox 裁出读数区并放大。失败一律返回 None —— 复核是加分项,不能变成新的故障点。"""
    try:
        from io import BytesIO

        from PIL import Image  # type: ignore[import-not-found]

        img = Image.open(path)
        img = img.convert("RGB")
        W, H = img.size
        x0, y0, x1, y1 = (float(v) for v in box)
        x0, x1 = min(x0, x1), max(x0, x1)
        y0, y1 = min(y0, y1), max(y0, y1)
        if x0 >= 1 or y0 >= 1 or x1 <= 0 or y1 <= 0:
            return None  # 框整个在图外:往外扩也只是扩出一块不相干的边角
        # 【往外放一圈】模型给的框常常贴着数字边缘,裁太紧会把首尾字符切掉半个,
        # 那样放大出来反而更难读。宁可多带一点背景。
        #
        # 【至少放出整张图的一截,不只按框的大小放】模型的框不光贴边,位置也常偏:
        # 2026-10-09 实测紫菡消防水表,框往上偏了半个窗口高,按框高放 25% 只多出几个像素,
        # 裁出来只有字轮的上半截 —— 复核读成 10,把第一遍的 102 改坏了(3 次 3 次)。
        # 偏差是按整张图算的,放宽也得按整张图:改成至少放出图宽 6%、图高 8% 后,3 次都没再截歪。
        bw, bh = x1 - x0, y1 - y0
        mx = max(bw * pad, SECOND_LOOK_MIN_PAD_X)
        my = max(bh * pad, SECOND_LOOK_MIN_PAD_Y)
        x0 -= mx
        x1 += mx
        y0 -= my
        y1 += my
        px0 = max(0, int(x0 * W))
        py0 = max(0, int(y0 * H))
        px1 = min(W, int(x1 * W))
        py1 = min(H, int(y1 * H))
        if px1 - px0 < 8 or py1 - py0 < 8:
            return None
        crop = img.crop((px0, py0, px1, py1))
        # 放大到长边 target_edge。只放大不缩小 —— 本来就大的框不用动。
        longest = max(crop.size)
        if longest < target_edge:
            ratio = target_edge / longest
            crop = crop.resize(
                (max(1, int(crop.size[0] * ratio)), max(1, int(crop.size[1] * ratio))),
                Image.LANCZOS,
            )
        out = BytesIO()
        crop.save(out, format="JPEG", quality=92, optimize=True)
        # 【裁歪了是这套东西最难查的失效方式】框偏一点,放大后送进去的就是
        # 另一块数字,而模型照样自信地读出来 —— 结果比不复核还糟。
        # 出问题时把裁剪结果落盘,才能一眼看出是框的问题还是读的问题。
        dump = os.environ.get("SECOND_LOOK_DUMP_DIR", "")
        if dump:
            try:
                os.makedirs(dump, exist_ok=True)
                name = f"crop_{os.path.basename(path)[:24]}_{px0}_{py0}.jpg"
                with open(os.path.join(dump, name), "wb") as fh:
                    fh.write(out.getvalue())
            except Exception:
                pass  # 存不下不影响复核
        return out.getvalue()
    except ImportError:
        return None
    except Exception as exc:
        print(f"[second-look] crop failed: {exc}", file=sys.stderr)
        return None


def _same_number(a: str, b: str) -> bool:
    try:
        return abs(float(a) - float(b)) < 1e-9
    except (TypeError, ValueError):
        return a.strip() == b.strip()


def collect_crop_targets(payload: dict, parsed: dict, fields: list) -> list:
    """挑出值得复核的读数:kind=number、给了合法 bbox、图也找得到。返回 [(code, 第一遍的值, jpeg)]"""
    number_codes = {
        str(f.get("code", "")).strip()
        for f in fields
        if str(f.get("kind", "")).strip() == "number"
    }
    number_codes.discard("")
    if not number_codes:
        return []
    images = payload.get("images") or []
    todo = []
    for item in parsed.get("recognizedFields") or []:
        if not isinstance(item, dict):
            continue
        code = str(item.get("code", "")).strip()
        if code not in number_codes or not _valid_bbox(item.get("bbox")):
            continue
        try:
            idx = int(item.get("imageIndex", 0))
        except (TypeError, ValueError):
            continue
        if not (1 <= idx <= len(images)):
            continue
        path = (images[idx - 1] or {}).get("path") or ""
        if not path or not os.path.exists(path):
            continue
        blob = crop_reading_area(path, item.get("bbox"))
        if blob:
            todo.append((code, str(item.get("value", "")).strip(), blob))
    return todo


def unverified_readings(parsed: dict, fields: list, checked_codes: set) -> list:
    """填了值、但没能进复核的读数字段(中文名)。"""
    label_of = {}
    number_codes = set()
    for f in fields:
        code = str(f.get("code", "")).strip()
        if not code:
            continue
        label_of[code] = str(f.get("label", "")).strip() or code
        if str(f.get("kind", "")).strip() == "number":
            number_codes.add(code)
    out = []
    for item in parsed.get("recognizedFields") or []:
        if not isinstance(item, dict):
            continue
        code = str(item.get("code", "")).strip()
        if (code in number_codes and code not in checked_codes
                and str(item.get("value", "")).strip()):
            out.append(label_of.get(code, code))
    return out


# 放大复核至少要留这么多时间。不够就不做 —— 做到一半被 Go 那边放弃,
# 第一遍的结果也跟着丢了,比不复核更糟。
SECOND_LOOK_MIN_SECONDS = 15


def second_look(payload: dict, parsed: dict, fields: list, api_key: str, scenario: str = "",
                deadline: float | None = None) -> dict:
    """对第一遍读出的 number 字段做裁剪复核。任何一步出问题都原样返回 parsed。"""
    if os.environ.get("SECOND_LOOK", "1") != "1":
        return parsed
    if not (parsed.get("recognizedFields") or []):
        return parsed
    todo = collect_crop_targets(payload, parsed, fields)
    left = seconds_left(deadline)
    if todo and left is not None and left < SECOND_LOOK_MIN_SECONDS:
        # 【没时间复核也要说出来】和"没框、没复核"一样:界面上看着和复核过的一样
        warns = parsed.setdefault("warnings", [])
        if isinstance(warns, list):
            labels = [code for code, _, _ in todo]
            warns.append("识别耗时较长，未做放大复核，请对照照片核实：" + "、".join(labels[:3]))
        print(f"[second-look] 剩余 {left:.0f}s,跳过复核 {len(todo)} 项", file=sys.stderr)
        return parsed
    # 【没被复核到的读数要说出来,不能静默放行】
    #
    # 能不能复核取决于模型有没有给出合法的 bbox。给不出的话,那个读数
    # 一次复核都没经过就进了记录 —— 而界面上和经过复核的长得一模一样。
    # 这种"保护看着在、其实没生效"的状态,比没有保护更危险。
    unchecked = unverified_readings(parsed, fields, {c for c, _, _ in todo})
    if unchecked:
        warns = parsed.setdefault("warnings", [])
        if isinstance(warns, list):
            warns.append("未经放大复核，请对照照片核实：" + "、".join(unchecked[:3]))
        print(f"[second-look] 有 {len(unchecked)} 项没框、未复核: {unchecked}", file=sys.stderr)
    if not todo:
        return parsed

    lines = [f"第 {i + 1} 张 → 字段 {code}" for i, (code, _, _) in enumerate(todo)]
    content: list = [{
        "type": "text",
        "text": "下面每张图都是一个读数区的放大特写,按顺序对应:\n"
                + "\n".join(lines)
                + "\n\n把每张图里的数字读出来。",
    }]
    for _, _, blob in todo:
        content.append({
            "type": "image_url",
            "image_url": {"url": "data:image/jpeg;base64," + base64.b64encode(blob).decode("ascii")},
        })

    system = SECOND_LOOK_SYSTEM
    if scenario.strip():
        system += SECOND_LOOK_SCENE_HEADER + scenario.strip()
    model_name = os.environ.get("QWEN_VISION_MODEL", "qwen-vl-plus")
    extra = {"enable_thinking": False} if model_name.lower().startswith("qwen3") else None
    try:
        raw = call_qwen_chat(
            model=model_name,
            system=system,
            user_content=content,
            api_key=api_key,
            timeout=int(os.environ.get("QWEN_VISION_TIMEOUT", "90") or "90"),
            max_retries=0,
            extra_body=extra,
            deadline=deadline,
            purpose="second-look",
        )
        checked = parse_json_response(raw)
    except Exception as exc:
        # 【复核挂了不影响识别】它是加分项,不是业务前提。
        print(f"[second-look] failed, keeping first pass: {exc}", file=sys.stderr)
        return parsed

    by_code = {}
    for r in (checked.get("readings") or []):
        if isinstance(r, dict) and str(r.get("code", "")).strip():
            by_code[str(r.get("code")).strip()] = r

    first_values = {code: val for code, val, _ in todo}
    label_of = {
        str(f.get("code", "")).strip(): str(f.get("label", "")).strip()
        for f in fields if str(f.get("code", "")).strip()
    }
    conflicted: list = []
    dropped: list = []
    for item in parsed.get("recognizedFields") or []:
        if not isinstance(item, dict):
            continue
        code = str(item.get("code", "")).strip()
        if code not in first_values:
            continue
        r = by_code.get(code)
        if not r:
            # 【放大后读不出 = 撤掉这个值,不是压一压置信度就算了】
            #
            # 实测:把那块暗屏单独裁出来增强后送进去,模型连着 4 次都答
            # "读不出";而同一张照片混在 6 张里整批送时,它填了 11661.941
            # 还给 0.7 —— 它不是眼睛不行,是【字段清单在逼它填空】:
            # 清单里摆着 Z1~Z4 四格,画面里有四块电表,顺序假设一上来
            # 就得给每格配一个数。
            #
            # 第二遍看的是放大的、专门的、没有别的字段干扰的特写。
            # 它说读不出,那就是真读不出,第一遍那个值是凑出来的。
            #
            # 【为什么敢清,而不是留着让人判断】线上实测过:三千多个字段里
            # 人改过 AI 的值 0 次 —— 留一个看着合理的错值,它就会被确认掉。
            # 空着反而逼人去看照片。原值仍在 aiValue 和 reason 里,
            # 想要回来随时能填。
            #
            # 【撤销的理由要写进 warnings,不能只写在字段上】
            # normalize_recognized_fields 会把空值的字段整条丢掉,
            # 写在 item["reason"] 里的话传不到记录 —— 现场只看到一个空格子,
            # 不知道 AI 试过、也不知道为什么放弃。
            old = str(item.get("value", "")).strip()
            item["value"] = ""
            item["confidence"] = 0
            # 【给现场看的是中文字段名,不是 code】模型不一定回 label,
            # 从字段清单里查一个 —— 现场看到 "z2_reading" 只会一脸茫然。
            dropped.append(f"{label_of.get(code) or item.get('label') or code}(整图读作{old})")
            continue
        new_val = str(r.get("value", "")).strip()
        if not new_val:
            continue
        old_val = first_values[code]
        if _same_number(new_val, old_val):
            item["reason"] = (str(item.get("reason", "")) + ";放大复核一致")[:80]
            continue
        # 【两遍对不上就清空,哪一边都不采信】(2026-10-09 改)
        # 原来采信放大那次、置信度压到 0.55。10/08 紫菡那组照片实测(每种做法跑 2~3 次):
        # 两遍对不上的十几次里,两个数全是错的 —— 整图读作 722(把提示词里的示例抄了)、
        # 放大读作 102;整图 211、放大 21(框歪了,只截到半个窗口)。而 0.55 的数照样会被
        # 人点确认。宁可空着让人对着照片填,也不交一个两遍都没对上的数。
        # 两次各读成什么写进 warnings —— 空值的字段会被整条丢掉,写在字段上传不到记录。
        item["value"] = ""
        item["confidence"] = 0
        conflicted.append(f"{label_of.get(code) or item.get('label') or code}(整图读作{old_val},放大读作{new_val})")

    warnings = parsed.setdefault("warnings", [])
    if isinstance(warnings, list):
        if conflicted:
            warnings.append("放大复核和整图读数对不上，已清空，请人工填写：" + "、".join(conflicted[:3]))
        if dropped:
            warnings.append("放大后读不出已撤销，请人工填写：" + "、".join(dropped[:3]))
    print(f"[second-look] 复核 {len(todo)} 项,对不上清空 {len(conflicted)} 项,读不出撤销 {len(dropped)} 项",
          file=sys.stderr)
    return parsed


# ===== /analyze =====


# 识别的总时间预算。移动端最多等 80 秒,Go 等 budget+10 秒 —— 三者对齐,
# 不再出现"手机早就说失败了,这边还在调模型"。
ANALYZE_BUDGET_SECONDS = 75


def analyze(payload: dict) -> dict:
    api_key = get_api_key()
    deadline = budget_deadline(payload, ANALYZE_BUDGET_SECONDS)
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
            max_retries=1,
            extra_body=extra,
            deadline=deadline,
            purpose="analyze",
        )
    except Exception as exc:
        print(f"[analyze] qwen failed: {exc}", file=sys.stderr)
        acct = account_error_of(exc)
        if acct:
            # 账号级故障:别叫用户"重拍" —— 重拍一百次也是同样结果,
            # 只会让他反复白干。直接说清是服务的问题,并给手动填写这条路。
            print(f"[analyze] ACCOUNT ERROR {acct} —— AI 服务整体不可用", file=sys.stderr)
            resp = retake_required(ACCOUNT_ERROR_HINT, payload)
            resp["accountError"] = acct
            return resp
        return retake_required(f"AI 调用失败：{str(exc)[:80]}，请重拍或转人工", payload)

    try:
        parsed = parse_json_response(raw)
    except Exception as exc:
        print(f"[analyze] parse failed: {exc}\nraw: {raw[:300]}", file=sys.stderr)
        return retake_required("AI 输出无法解析为 JSON，请重拍", payload)

    # 读数区裁出来放大再核一遍。整个过程包在 second_look 里 fail-safe:
    # 裁不出来、模型没回、解析失败,一律原样返回第一遍的结果。
    # scenario 必须传进去 —— 第二遍不带场景规则会只读 LCD 的其中一行。
    parsed = second_look(payload, parsed, fields, api_key, scenario, deadline=deadline)

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
        item = {
            "code": code,
            "label": str(f.get("label", "")),
            "value": value,
            "confidence": max(0.0, min(confidence, 1.0)),
            "reason": reason[:80],
        }
        # 【这个读数是从哪张图、哪一块读出来的,要一路带到确认页】
        #
        # 现场是按顺序拍的:四块电表一字排开挨个拍。AI 也按顺序配,
        # 中间夹一张读不出的就整体错位一格 —— 读数一个不差、全填错了格子。
        #
        # 人要纠正它,前提是能看见"这个数是从哪张图来的"。现在确认页上
        # 只有一个光秃秃的数字,人没有参照物,只能靠记忆去对六张照片 ——
        # 所以实际发生的是不对,直接确认(线上三千多个字段,人改过 0 次)。
        #
        # bbox 带上之后,界面能直接把那一小块读数区裁出来摆在这一行旁边,
        # 一眼就知道对不对,不用点开大图再翻页找。
        if _valid_bbox(f.get("bbox")):
            try:
                idx = int(f.get("imageIndex", 0))
            except (TypeError, ValueError):
                idx = 0
            if idx >= 1:
                item["imageIndex"] = idx
                item["bbox"] = [round(float(v), 4) for v in f.get("bbox")]
        out.append(item)
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
    # 预算是给调用层的,不是给模型看的
    user_text = json.dumps({k: v for k, v in payload.items() if k != "budgetSeconds"}, ensure_ascii=False)
    try:
        raw = call_qwen_chat(
            model=text_model,
            system=SUMMARY_PROMPT,
            user_content=user_text,
            api_key=api_key,
            timeout=10,
            max_retries=1,
            deadline=budget_deadline(payload, 25),
            purpose="summarize",
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


# ===== 让总结读得懂 =====
#
# 【领导读的是结论,不是表单】原来这里拼出来的是 "电梯使用登记标志完好=否",
# 那是表单原文,不是一句话。而且踩了三个更实的坑:
#
#   1. "不符合项处理情况记录" 是一条【文本记录】,不是判定项。它的正文里
#      带着"缺失"两个字,于是被当成第 4 个问题算了一遍 —— 同一段话里
#      开头写"待跟进 4 项"、结尾写"发现 3 项不符合项",自己跟自己打架。
#      读到这儿的人不会去分辨谁对,他会不再信这段话。
#   2. 那条记录的正文是 `reg_mark:缺失使用标志; alarm_device:未见报警装置`
#      这种形式 —— 字段代码直接倒给人看。
#   3. "未拍操作面板"是【照片没拍到】,不是设备坏了。混进不符合项里会把
#      问题数量报高,而模板提示词里明写着"缺照片 ≠ 设备异常"。

# 不参与"问题计数"的字段:它们是记录/说明,不是判定项。
SUMMARY_RECORD_CODES = ("nonconformity",)

# 照片没拍到 ≠ 设备异常。这类要单独说,不能混进不符合项。
PHOTO_GAP_PHRASES = ("未拍", "没拍", "未提供", "无照片", "缺照片", "补拍", "看不清", "未拍到")

# 正向问法的结尾词 → 判"否"时的人话说法
NEGATED_SUFFIX = (
    ("完好", "不完好"),
    ("正常", "异常"),
    ("有效", "失效"),
    ("齐全", "不齐全"),
    ("完整", "不完整"),
    ("合格", "不合格"),
    ("通过", "未通过"),
)


def summary_field_code(field: dict) -> str:
    return str(field.get("code") or "").strip()


def summary_is_record_field(field: dict) -> bool:
    """这一条是"记录/说明"而不是判定项。"""
    if summary_field_code(field) in SUMMARY_RECORD_CODES:
        return True
    # 老记录可能没带 code,退回按标签认
    return "不符合项" in summary_field_label(field)


def parse_nonconformity(text: str) -> dict:
    """把 `code:描述; code:描述` 拆成 {code: 描述}。

    【这串东西其实是结构化数据】模型按提示词逐条写问题时,会带上字段代码。
    拆开之后每个问题的人话描述就有了 —— 比 "某某完好=否" 具体得多。
    拆不出来(模型写成了整句)就返回空,调用方回退到标签改写。
    """
    out = {}
    for part in re.split(r"[;；]", text or ""):
        m = re.match(r"^\s*([A-Za-z_][A-Za-z0-9_]*)\s*[:：]\s*(.+?)\s*$", part)
        if m:
            out[m.group(1)] = m.group(2).strip(" 。;；,，")
    return out


def summary_is_photo_gap(text: str) -> bool:
    return any(p in (text or "") for p in PHOTO_GAP_PHRASES)


def humanize_abnormal(label: str, value: str) -> str:
    """把 "X完好" + "否" 说成 "X不完好",而不是 "X完好=否"。"""
    for pos, neg in NEGATED_SUFFIX:
        if label.endswith(pos) and value in ("否", "无"):
            return label[: -len(pos)] + neg
    if value in ("否", "无"):
        return f"{label}不符合"
    return f"{label}:{value}"


def summarize_field_findings(payload: dict) -> tuple[list[str], list[str]]:
    """返回 (真正的不符合项, 因照片缺失无法判定的项),都是人话。"""
    fields = [f for f in (payload.get("fields") or []) if isinstance(f, dict)]

    # 先把"不符合项记录"里的逐条描述取出来,它比标签具体
    detail = {}
    for f in fields:
        if summary_is_record_field(f):
            detail.update(parse_nonconformity(summary_field_value(f)))

    abnormal: list[str] = []
    photo_gaps: list[str] = []
    for field in fields:
        # 【记录字段本身不算一个问题】它是对上面那些问题的说明
        if summary_is_record_field(field):
            continue
        label = summary_field_label(field)
        value = summary_field_value(field)
        if not label:
            continue
        if not value:
            if field.get("required"):
                abnormal.append(f"{label}未填写")
            continue
        if not summary_value_is_abnormal(label, value):
            continue
        # 有逐条描述就用描述("缺失使用标志"),没有才退回标签改写
        desc = detail.get(summary_field_code(field), "")
        if summary_is_photo_gap(desc):
            photo_gaps.append(desc or label)
        else:
            abnormal.append(desc or humanize_abnormal(label, value))

    note = str(payload.get("abnormalNote") or "").strip()
    if note and not any(p in note for p in ("无异常", "未发现异常", "没有异常")):
        abnormal.append(note[:30])
    return abnormal, photo_gaps


def summarize_abnormal_fields(payload: dict) -> list[str]:
    """只要不符合项(不含照片缺失)。给"要不要标异常"这类判断用。"""
    abnormal, _ = summarize_field_findings(payload)
    return abnormal


def enforce_summary_policy(summary: str, payload: dict, abnormal_fields: list[str] | None = None) -> str:
    abnormal_fields = abnormal_fields if abnormal_fields is not None else summarize_abnormal_fields(payload)
    summary = re.sub(r"\s+", " ", summary or "").strip()
    if not summary:
        # 模型没给正文时的兜底。
        #
        # 【不要再把字段倒一遍】原来这里拼的是 "关键字段:某某完好=否; 某某有效=否;…",
        # 表单原文照抄一份 —— 既和开头那句重复,又是给读表单的人看的东西。
        # 兜底要做的是把"谁、什么时候、哪台、其余是否正常"说清楚,仅此而已。
        normal = [
            summary_field_label(f)
            for f in (payload.get("fields") or [])
            if isinstance(f, dict)
            and not summary_is_record_field(f)
            and summary_field_value(f)
            and not summary_value_is_abnormal(summary_field_label(f), summary_field_value(f))
        ]
        where = payload.get("pointName") or payload.get("project") or ""
        summary = (
            f"{payload.get('inspectionTime', '')}"
            f"{('，' + where) if where else ''}"
            f"由{payload.get('inspector', '巡检员')}完成{payload.get('templateName', '巡检')}。"
        )
        if normal:
            summary += f"其余各项（{'、'.join(normal[:4])}{'等' if len(normal) > 4 else ''}）均正常。"

    # 去掉模型/历史可能写的旧式"异常提示：…""低风险总结：…"前后缀（历史提示词遗留，啰嗦）
    summary = re.sub(r"^异常提示[:：][^。；;]*[。；;]?\s*", "", summary).strip()
    summary = re.sub(r"\s*低风险总结[:：].*$", "", summary).strip()
    summary = summary.lstrip("。；; ").strip()
    # 【开头一句话交代结论,而且只交代一次】
    #
    # 原来这里写 "待跟进 N 项:某某完好=否、…",而正文里模型又会写一遍
    # "本次巡检发现 3 项不符合项" —— 两个数还经常对不上(记录字段被多算了一个)。
    # 同一段话里两个互相矛盾的数字,读的人不会去分辨谁对,他会不再信这段话。
    #
    # 所以:数只由这里给,措辞和正文统一成"不符合";照片缺失单独说一句,
    # 不混进那个数里。
    _, photo_gaps = summarize_field_findings(payload)
    head = ""
    if abnormal_fields:
        labels = "、".join(abnormal_fields[:4])
        if len(abnormal_fields) > 4:
            labels += "等"
        head = f"本次发现 {len(abnormal_fields)} 项不符合：{labels}。"
    if photo_gaps:
        # 【和不符合项分开】"没拍到"是照片问题,不是设备坏了。混在一起会把
        # 问题数量报高,而模板提示词里明写着"缺照片 ≠ 设备异常"。
        head += f"另有 {len(photo_gaps)} 项因照片缺失无法判定：{'、'.join(photo_gaps[:3])}。"
    if head:
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


def render_scene_prompt(candidates: list) -> str:
    """把后端下发的候选表拼进场景分类提示词。

    【为什么候选表要由后端下发】以前它写死在 prompts/scene_classifier.md 里,
    提示词还明写着"必须从这个清单选一个" —— 后台新建的模板不在清单里,
    模型返回不了它的 id。更糟的不是认不出,是认错:模型被要求必须选一个,
    于是挑一个最像的旧模板返回,界面显示"识别成功",现场按另一套规则巡检。

    【下发为空就回退用内置那份】和 promptText 一样的灰度策略:下发出问题时
    退回到今天这个能跑的状态,而不是一个场景都认不出来。
    """
    rows = []
    for c in candidates:
        if not isinstance(c, dict):
            continue
        tid = str(c.get("templateId") or "").strip()
        if not tid:
            continue
        name = str(c.get("templateName") or "").strip()
        # 竖线会把表格切错列,换行会把一行拆成两行 —— 两者都会让这一行
        # 变成模型读不懂的噪音,而它恰恰是这个场景唯一的判断依据。
        feat = str(c.get("features") or "").strip()
        feat = feat.replace("|", "/").replace("\n", " ").replace("\r", " ")
        rows.append("| `%s` | %s | %s |" % (tid, name, feat))
    if not rows:
        return SCENE_PROMPT

    table = "\n".join(
        ["| templateId | 描述 | 图片特征 |", "| --- | --- | --- |"]
        + rows
        # unknown 必须留着:没有"我不知道"这个出口,模型只能在几个都不像的
        # 场景里硬挑一个,而挑错不会报错。
        + ["| `unknown` | 无法判断 | 拍到墙、地、天花板、人手、与巡检无关的物体 |"]
    )

    marker = "## 候选模板（必须从这个清单选一个）"
    head, sep, tail = SCENE_PROMPT.partition(marker)
    if not sep:
        # 提示词里找不到那一节 —— md 被改过。此时【不能静默回退到内置表】,
        # 那会让新建的模板继续认不出来,而且没人知道为什么。
        # 把候选表接在最后,并明说以它为准。
        return SCENE_PROMPT + "\n\n## 候选模板（以下表为准,必须从中选一个）\n\n" + table

    # 原表格一直到下一节标题之间的内容整段换掉
    rest = tail.split("\n## ", 1)
    out = head + marker + "\n\n" + table + "\n"
    if len(rest) == 2:
        out += "\n## " + rest[1]
    return out


# 场景分类是现场拍完照同步等的,Go 等 budget+5 秒
CLASSIFY_BUDGET_SECONDS = 30
# 低于这个置信度一律让人选 —— 不管模型自己说要不要
CLASSIFY_AUTO_MIN_CONFIDENCE = 0.7


def classify_needs_manual(template_id: str, confidence: float, model_says) -> bool:
    """要不要让现场人手动选模板。

    【模型说"不用"不算数】原来直接采信模型自报的 needsManualPick:它给 0.3 的
    置信度、同时说不用人工选,就真的不让人选了。自动化只在"认得出、又有把握"
    时才生效,其余一律交给人 —— 少一次自动化,好过一次错的自动化。
    """
    return bool(model_says) or template_id == "unknown" or confidence < CLASSIFY_AUTO_MIN_CONFIDENCE


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

    # 候选表由后端按库里的模板生成,随请求下发
    scene_prompt = render_scene_prompt(payload.get("candidates") or [])
    content: list = [{"type": "text", "text": scene_prompt}]
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
            deadline=budget_deadline(payload, CLASSIFY_BUDGET_SECONDS),
            purpose="classify",
        )
    except Exception as exc:
        print(f"[classify] qwen failed: {exc}", file=sys.stderr)
        acct = account_error_of(exc)
        if acct:
            # 账号级故障:说人话,并标出来给上层用(后台挂横幅、运维告警)
            print(f"[classify] ACCOUNT ERROR {acct} —— AI 服务整体不可用", file=sys.stderr)
        return {
            "templateId": "unknown",
            "templateName": "无法识别",
            "confidence": 0,
            "reason": ACCOUNT_ERROR_HINT if acct else f"AI 调用失败：{str(exc)[:60]}",
            "accountError": acct,
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

    template_id = str(parsed.get("templateId", "unknown")).strip() or "unknown"
    try:
        confidence = float(parsed.get("confidence", 0))
    except (TypeError, ValueError):
        confidence = 0.0  # 模型偶尔回"高"这种字 —— 当作没把握
    needs_manual = classify_needs_manual(template_id, confidence, parsed.get("needsManualPick"))
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


def call_deepseek_tools(
    *,
    model: str,
    messages: list,
    tools: list,
    api_key: str,
    timeout: int = 30,
    deadline: float | None = None,
) -> dict:
    """带工具的一轮对话。返回 {finish, reply?, toolCalls?, model}。

    和 call_deepseek_chat 的区别:那个只发 system+user 两条、只要文本;
    这个发【完整消息数组】(含工具返回),并且要能拿回 tool_calls。
    【不重试】循环在 Go 那边,一轮失败 Go 会退回不带工具的老路 ——
    这里再重试一次,只会把老路剩下的时间吃掉。
    """
    base_url = os.environ.get("DEEPSEEK_BASE_URL", "https://api.deepseek.com").rstrip("/")
    body = {
        "model": model,
        "messages": messages,
        "temperature": 0.2,
    }
    if tools:
        body["tools"] = tools
        body["tool_choice"] = "auto"
    data = post_chat_completion(
        provider="deepseek", url=f"{base_url}/chat/completions", body=body, api_key=api_key,
        timeout=timeout, max_retries=0, deadline=deadline, purpose="management-tools",
    )
    LAST_CHAT_ERROR.clear()
    msg = data["choices"][0].get("message") or {}
    actual_model = data.get("model") or model
    calls = msg.get("tool_calls") or []
    if calls:
        # 【原样回传 id】后面那一轮的 tool 消息要靠它对应回来,
        # 少了或改了,模型就不知道这条结果是哪次调用的。
        return {
            "finish": "tool_calls",
            "model": actual_model,
            "assistantMessage": msg,
            "toolCalls": [
                {
                    "id": c.get("id") or "",
                    "name": ((c.get("function") or {}).get("name") or ""),
                    "arguments": ((c.get("function") or {}).get("arguments") or "{}"),
                }
                for c in calls
            ],
        }
    return {
        "finish": "stop",
        "model": actual_model,
        "reply": (msg.get("content") or "").strip(),
    }


def management_chat_tools(payload: dict) -> dict:
    """工具调用的一轮。循环由 Go 侧驱动 —— 工具是 Go 函数,数据和权限都在那边;
    这里只负责"把消息和工具清单交给模型,把模型的决定原样带回去"。
    """
    key = get_deepseek_key()
    if not key:
        return {"finish": "error", "message": "DeepSeek 未配置"}
    model = os.environ.get("DEEPSEEK_CHAT_MODEL", "deepseek-chat")
    timeout = int(os.environ.get("DEEPSEEK_TIMEOUT_SECONDS", "30") or "30")
    messages = payload.get("messages") or []
    tools = payload.get("tools") or []
    if not messages:
        return {"finish": "error", "message": "messages 不能为空"}
    try:
        return call_deepseek_tools(
            model=model, messages=messages, tools=tools,
            api_key=key, timeout=timeout, deadline=budget_deadline(payload, 25),
        )
    except Exception as exc:
        chat_account_error_of(exc)
        print(f"[management/chat-tools] failed: {exc}", file=sys.stderr)
        return {"finish": "error", "message": str(exc)[:200]}


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
            model=model, system=management_chat_system(payload),
            user_content=user_text, api_key=key, timeout=timeout,
            deadline=budget_deadline(payload, 25), purpose="management-chat",
        )
    except Exception as exc:
        # 记下账号级故障,/health 才看得见 —— 不记的话,欠费时界面
        # 和一切正常时长得一模一样。
        chat_account_error_of(exc)
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
        # 【不冒充真模型】原来写的是 deepseek-v4-flash,日志和排查时看着像真回答
        "model": "rule-fallback",
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
    user_text = json.dumps({k: v for k, v in payload.items() if k != "budgetSeconds"}, ensure_ascii=False)
    timeout = int(os.environ.get("DEEPSEEK_TIMEOUT_SECONDS", "30") or "30")
    try:
        reply, actual_model = call_deepseek_chat(
            model=model, system=system,
            user_content=user_text, api_key=key, timeout=timeout,
            deadline=budget_deadline(payload, 30), purpose="management-analyze",
        )
    except Exception as exc:
        chat_account_error_of(exc)
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
        "model": "rule-fallback",
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
            # 账号级故障(欠费/key 失效/未开通)—— key 存在不代表能用,
            # 这一条才是"为什么识别全都失败"的答案
            if LAST_ACCOUNT_ERROR:
                degraded_reasons.append("account_" + LAST_ACCOUNT_ERROR.get("code", "error"))
            # 问答账户单独报 —— 和视觉是两个账户,合并会把"识别还能用"抹掉
            if LAST_CHAT_ERROR:
                degraded_reasons.append("chat_" + LAST_CHAT_ERROR.get("code", "error"))
            write_json(self, 200, {
                "status": "ok",
                "service": "ai-service",
                # Keep hasKey for compatibility with the existing local checks.
                "hasKey": has_dashscope_key,
                "hasDashscopeKey": has_dashscope_key,
                "hasVisionKey": has_dashscope_key,
                # 有值 = AI 整体不可用,不是个别照片识别不出来
                "accountError": LAST_ACCOUNT_ERROR or None,
                # 管理问答(DeepSeek)的账号级故障。有值 = 每一句回答都是兜底文案
                "chatError": LAST_CHAT_ERROR or None,
                "hasDeepSeekKey": has_deepseek_key,
                "managementAIReady": has_deepseek_key,
                "managementAI": "deepseek" if has_deepseek_key else "rule_fallback",
                "degradedReason": ",".join(degraded_reasons),
                "promptsLoaded": len(TEMPLATE_PROMPT_MAP) + 3,  # +common+scene+summary
            })
            return

        # /prompt-source?template=xxx —— 把内置的那份提示词正文交出去。
        #
        # 【为什么要这个接口】后台要让人编辑提示词,就得先给他看到"现在用的
        # 是什么"。而这份正文只有 ai-service 手里有(在 prompts/*.md)。
        # 把文本抄一份到 Go 里也能做,但两份文本一定会走散 —— 到时候后台
        # 显示的和模型实际收到的不是同一段,而人是照着后台那段在调。
        if self.path.startswith("/prompt-source"):
            query = parse_qs(urlparse(self.path).query)
            template_id = (query.get("template") or [""])[0]
            paper_ocr = (query.get("paperOCR") or [""])[0] in ("1", "true", "yes")
            try:
                text = prompt_for_template(template_id, paper_ocr=paper_ocr) or ""
            except OSError as exc:  # 文件被删/权限问题,不该让后台整页挂掉
                print(f"[prompt-source] {template_id}: {exc}", file=sys.stderr)
                text = ""
            write_json(self, 200, {
                "template": template_id,
                "found": bool(text),
                "prompt": text,
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
            elif self.path == "/management/chat":
                write_json(self, 200, management_chat(payload))
            elif self.path == "/management/chat-tools":
                write_json(self, 200, management_chat_tools(payload))
            elif self.path == "/prompt/draft-fields":
                write_json(self, 200, draft_fields(payload))
            elif self.path == "/management/analyze":
                write_json(self, 200, management_analyze(payload))
            else:
                write_json(self, 404, {"error": "not_found"})
        except Exception as exc:
            print(f"[handler] {self.path} error: {exc}", file=sys.stderr)
            import traceback
            traceback.print_exc()
            write_json(self, 500, {"error": "internal", "message": str(exc)[:200]})



# ===== 需求文字 → 字段表 =====
#
# 【为什么产出的是字段表,不是一整段提示词】
# 系统里已经有"字段表 → 标准提示词"的渲染器(Go 侧 renderPromptText),
# 它就是那份 skill 的代码化:总则 / 字段映射 / 输出 / 置信度。
# 让模型直接写整段提示词的话,要再解析回字段表才能编辑 —— 而自然语言
# 解析回结构化数据是有损的,解析不出来的部分会静默丢掉。
# 反过来先出字段表、再由渲染器生成提示词,两边都是无损的,
# 而且人看到的提示词和模型将来收到的一字不差。

DRAFT_FIELDS_SYSTEM = """你在为一套设备巡检系统设计"检查项字段表"。用户会用一段话描述他要检查什么,你把它拆成结构化的检查项。

只输出 JSON,不要任何解释文字、不要 markdown 代码块。格式:
{"sceneFeatures":"...","fields":[{"code":"...","label":"...","kind":"choice","judgeMode":"visual","yesWhen":"...","noWhen":"...","skipWhen":"...","note":""}]}

sceneFeatures —— 一句话说清"这个场景的照片长什么样",让人拍完照能一眼认出是这里。
写【看得见的实物和颜色组合】,不要写要检查什么。一句话,30 字左右。
  好:红色消防泵 + 不锈钢水箱 + 绿色环氧地坪
  好:灰白机房 + UPS 主机柜(带显示屏)+ 电池组排列 + 防静电地板
  差:检查消防泵是否正常运行(这是要检查什么,不是照片长什么样)
  差:泵房(太笼统,和别的泵房分不开)
【要能和相近场景分开】消防泵房和生活水泵房要拍的东西几乎一样(泵、水箱、
压力表),真正能一眼分开的是颜色:红泵+绿地坪 vs 蓝压力罐+银管道。
写不出区分点的话,这个场景会去抢别人的照片,而抢错了不会报错。

每一项的规矩:

- code:英文小写+下划线,见名知意(door_sign / room_clean / water_pressure)。
  【必须稳定】同一个含义在不同模板里要用同一个 code —— 温度一律 temperature,
  不要一会儿 temp 一会儿 temperature。读数趋势是按 code 归集的,不一致就断成两截。
- label:中文,就是巡检员在表单上看到的那一行字。
- kind:choice(是/否判断)/ number(读数)/ text(文字记录)。绝大多数检查项是 choice。
- judgeMode 从这几个里选,不要自创:
    visual          看外观/状态判是否 —— 最常用
    visual_lenient  主观项,少量瑕疵不算异常(卫生、整洁这类)
    read_text       读取铭牌/编号上的文字
    number          读数值(kind=number 时用)
    objective_date  读日期和当前日期比对(有效期、检验日期)
    functional_test 需要现场测试动作的照片(防夹、急停)
    sensory         靠听/闻,照片判不了 —— 留人工
    summary         汇总不符合项(每个模板末尾一条)
- yesWhen:判"是"时照片上应该看到什么,具体到能对着照片核对。
- noWhen:判"否"时看到什么。
- skipWhen:什么情况不返回、留给人工(通常是"没拍到该部位")。

硬要求:
1. 【第一项固定是设备编号】{"code":"asset_no","label":"设备编号","kind":"text","judgeMode":"read_text","yesWhen":"编号牌/铭牌上的编号清晰可读"}
   —— 没有它,提交的记录挂不到任何设备上。
2. 【最后一项固定是不符合项汇总】{"code":"nonconformity","label":"不符合项处理情况记录","kind":"text","judgeMode":"summary"}
3. 靠听觉嗅觉判断的(异响、异味、焦糊味)judgeMode 一律 sensory —— 照片判不了这些,
   让 AI 假装判得了,现场就会把"没证据"记成"正常"。
4. 检查项控制在 6-15 条。太少覆盖不住,太多现场没人拍得全,最后变成随便点。
5. 不要编用户没提到的检查项。宁可少,让人自己加。"""


def draft_fields(payload: dict) -> dict:
    """把一段需求描述拆成字段表。失败时返回 error,不编造。"""
    key = get_deepseek_key()
    requirement = (payload.get("requirement") or "").strip()
    if not requirement:
        return {"error": "requirement_empty", "message": "请先写一段需求描述"}
    if not key:
        # 【不给兜底字段表】编一份出来的话,人会以为 AI 读懂了他的需求,
        # 而实际拿到的是一份和需求无关的模板 —— 比直接说"没配密钥"糟得多。
        return {"error": "no_key", "message": "管理 AI 未配置密钥,无法生成"}

    parts = [f"要检查的内容:\n{requirement}"]
    if payload.get("templateName"):
        parts.append(f"模板名称:{payload['templateName']}")
    if payload.get("assetType"):
        parts.append(f"设备类型:{payload['assetType']}")

    model = os.environ.get("DEEPSEEK_CHAT_MODEL", "deepseek-chat")
    timeout = int(os.environ.get("DEEPSEEK_TIMEOUT_SECONDS", "30") or "30")
    try:
        reply, actual_model = call_deepseek_chat(
            model=model, system=DRAFT_FIELDS_SYSTEM,
            user_content="\n\n".join(parts), api_key=key, timeout=timeout,
            deadline=budget_deadline(payload, 80), purpose="draft-fields",
        )
    except Exception as exc:
        chat_account_error_of(exc)
        print(f"[prompt/draft-fields] deepseek failed: {exc}", file=sys.stderr)
        return {"error": "ai_failed", "message": str(exc)[:160]}

    fields, scene_features = _parse_draft_output(reply)
    if not fields:
        # 解析不出来就说实话。返回空字段表的话,界面上是"生成成功但一条都没有",
        # 人只会反复点生成。
        return {"error": "bad_output", "message": "AI 返回的内容解析不出字段表,请把需求写得更具体些再试"}
    # 【识别特征生成不出来不算失败】它只影响"拍完自动认场景"这一步,
    # 字段表本身照常可用;后台还能手工补一句。为它整个报错太重了。
    return {"fields": fields, "sceneFeatures": scene_features, "model": actual_model}


def _parse_draft_output(reply: str) -> tuple:
    """从模型回复里抠出 (字段表, 识别特征)。模型可能裹 ```json,也可能前后带话。"""
    text = (reply or "").strip()
    if "```" in text:
        # 取第一个代码块里的内容
        parts = text.split("```")
        for p in parts:
            p = p.strip()
            if p.startswith("json"):
                p = p[4:].strip()
            if p.startswith("{"):
                text = p
                break
    start = text.find("{")
    end = text.rfind("}")
    if start < 0 or end <= start:
        return [], ""
    try:
        data = json.loads(text[start:end + 1])
    except Exception:
        return [], ""
    fields = data.get("fields")
    feats = data.get("sceneFeatures")
    return (
        fields if isinstance(fields, list) else [],
        str(feats).strip() if isinstance(feats, str) else "",
    )


if __name__ == "__main__":
    host = os.environ.get("AI_SERVICE_ADDR", "127.0.0.1")
    port = int(os.environ.get("AI_SERVICE_PORT", "19100"))
    has_key = bool(get_api_key())
    print(f"AI service listening on http://{host}:{port}")
    print(f"  Has API key: {has_key}")
    print(f"  Prompts dir: {PROMPTS_DIR}")
    httpd = ThreadingHTTPServer((host, port), Handler)
    httpd.serve_forever()
