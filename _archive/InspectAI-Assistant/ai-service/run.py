import json
import os
import base64
import re
import time
import uuid
from datetime import datetime, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib import request
from urllib.error import HTTPError, URLError


def now_iso():
    return datetime.now(timezone.utc).astimezone().isoformat()


def write_json(handler, status, payload):
    body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json; charset=utf-8")
    handler.send_header("Content-Length", str(len(body)))
    handler.end_headers()
    handler.wfile.write(body)


def read_json(handler):
    length = int(handler.headers.get("Content-Length", "0") or "0")
    raw = handler.rfile.read(length)
    if not raw:
        return {}
    return json.loads(raw.decode("utf-8"))


def mock_analysis(payload):
    record_id = payload.get("recordId") or f"rec_{uuid.uuid4().hex[:8]}"
    images = payload.get("images") or []
    template = payload.get("template") or {}
    fields = template.get("fields") or []
    provider = os.environ.get("AI_PROVIDER", "mock")
    model_name = os.environ.get("QWEN_VISION_MODEL", "qwen3.6-plus")

    if provider == "mock":
        model_name = "mock-vision-provider"

    force_fail = any("fail" in (image.get("fileName") or "").lower() for image in images)
    result_images = []
    for idx, image in enumerate(images or [{"imageId": "img_demo"}], start=1):
        image_id = image.get("imageId") or image.get("id") or f"img_{idx:03d}"
        file_name = image.get("fileName") or "demo.jpg"
        lower_name = file_name.lower()

        if "meter" in lower_name or "表" in file_name:
            detected_type = "meter_reading"
            observations = [
                "图片中可见表计或设备显示区域",
                "读数区域较清晰，适合进入人工复核",
                "未发现明显遮挡"
            ]
            extracted = {
                "temperature": 24.6,
                "humidity": 48.0,
                "voltageA": None,
                "voltageB": None,
                "voltageC": None,
                "meterReading": 1286.5,
                "indicatorRed": None,
                "indicatorYellow": None,
                "indicatorGreen": None
            }
            findings = [
                {"code": "METER_VISIBLE", "label": "表计读数区域可见", "confidence": 0.82}
            ]
        else:
            detected_type = "environment_scene"
            observations = [
                "现场可见机房或设备区域",
                "地面未见明显积水",
                "通道未见明显杂物堆放",
                "设备外观未见明显破损"
            ]
            extracted = {
                "temperature": None,
                "humidity": None,
                "voltageA": None,
                "voltageB": None,
                "voltageC": None,
                "meterReading": None,
                "indicatorRed": None,
                "indicatorYellow": None,
                "indicatorGreen": "visible"
            }
            findings = [
                {"code": "NO_VISIBLE_WATER", "label": "未见明显积水", "confidence": 0.88},
                {"code": "NO_VISIBLE_CLUTTER", "label": "未见明显杂物堆放", "confidence": 0.81}
            ]

        result_images.append({
            "imageId": image_id,
            "detectedImageType": detected_type,
            "detectedScene": payload.get("point", {}).get("name", "巡检点位"),
            "observations": observations,
            "extractedFields": extracted,
            "visualFindings": findings,
            "quality": {
                "isBlurry": force_fail,
                "isOverExposed": False,
                "isRotated": False,
                "score": 0.31 if force_fail else 0.84
            },
            "confidence": 0.31 if force_fail else 0.84,
            "needReview": True
        })

    recognized = [] if force_fail else build_recognized_fields(fields, payload)

    return {
        "schemaVersion": "1.0",
        "analysisId": f"ana_{uuid.uuid4().hex[:10]}",
        "recordId": record_id,
        "model": {
            "provider": provider,
            "name": model_name,
            "version": "local-demo"
        },
        "processedAt": now_iso(),
        "durationMs": 620,
        "tokenUsage": {
            "input": 0,
            "output": 0,
            "imageCount": len(result_images)
        },
        "partialFailure": False,
        "failedImageIds": [],
        "images": result_images,
        "recognizedFields": recognized,
        "recognitionStatus": "retake_required" if force_fail else "recognized",
        "retakeReason": "图片质量不足，请重新拍摄" if force_fail else "",
        "warnings": [] if provider == "mock" else ["LOCAL_DEMO_PROVIDER_NO_EXTERNAL_CALL"]
    }


def analyze_with_qwen(payload):
    api_key = os.environ.get("DASHSCOPE_API_KEY", "").strip()
    if not api_key:
        raise RuntimeError("DASHSCOPE_API_KEY is empty")

    started = time.time()
    template = payload.get("template") or {}
    point = payload.get("point") or {}
    images = payload.get("images") or []
    fields = template.get("fields") or []
    model_name = os.environ.get("QWEN_VISION_MODEL", "qwen-vl-plus")

    field_lines = [
        {
            "code": field.get("code"),
            "label": field.get("label"),
            "kind": field.get("kind"),
            "options": field.get("options") or [],
            "required": field.get("required"),
        }
        for field in fields
    ]
    prompt = (
        "你是工程巡检图片识别助手。请只返回JSON，不要Markdown。"
        "任务：根据图片和日报字段模板，提取可确认字段；识别不稳定时要求重新拍照。"
        "JSON格式："
        "{\"recognitionStatus\":\"recognized或retake_required\","
        "\"retakeReason\":\"原因\","
        "\"observations\":[\"图片事实\"],"
        "\"recognizedFields\":[{\"code\":\"字段编码\",\"value\":\"字段值\",\"confidence\":0.0,\"reason\":\"依据\"}],"
        "\"warnings\":[\"提示\"]}。"
        "不要编造图片中无法判断的读数；人工字段可留空。"
        f"点位：{point.get('project', '')}/{point.get('name', '')}。"
        f"模板：{template.get('name', '')}。字段：{json.dumps(field_lines, ensure_ascii=False)}"
    )

    content = [{"type": "text", "text": prompt}]
    for image in images[:3]:
        data_url = image_to_data_url(image.get("path") or image.get("Path") or "")
        if data_url:
            content.append({"type": "image_url", "image_url": {"url": data_url}})

    if len(content) == 1:
        raise RuntimeError("no readable image for qwen vision call")

    response = call_openai_compatible_chat(model_name, content, api_key)
    parsed = parse_json_response(response)
    return build_analysis_from_model(payload, parsed, int((time.time() - started) * 1000), "dashscope", model_name)


def call_openai_compatible_chat(model_name, content, api_key):
    base_url = os.environ.get("DASHSCOPE_BASE_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1").rstrip("/")
    url = f"{base_url}/chat/completions"
    body = {
        "model": model_name,
        "messages": [
            {"role": "system", "content": "你只输出严格JSON。"},
            {"role": "user", "content": content},
        ],
        "temperature": 0.1,
    }
    data = json.dumps(body, ensure_ascii=False).encode("utf-8")
    req = request.Request(
        url,
        data=data,
        headers={
            "Authorization": f"Bearer {api_key}",
            "Content-Type": "application/json",
        },
        method="POST",
    )
    try:
        with request.urlopen(req, timeout=60) as resp:
            payload = json.loads(resp.read().decode("utf-8"))
    except HTTPError as exc:
        detail = exc.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"qwen http {exc.code}: {detail}") from exc
    except URLError as exc:
        raise RuntimeError(f"qwen network error: {exc.reason}") from exc

    choices = payload.get("choices") or []
    if not choices:
        raise RuntimeError("qwen response has no choices")
    message = choices[0].get("message") or {}
    return message.get("content") or ""


def parse_json_response(text):
    if not text:
        raise RuntimeError("qwen response content is empty")
    cleaned = text.strip()
    match = re.search(r"```(?:json)?\s*(.*?)```", cleaned, re.S)
    if match:
        cleaned = match.group(1).strip()
    first = cleaned.find("{")
    last = cleaned.rfind("}")
    if first >= 0 and last >= first:
        cleaned = cleaned[first:last + 1]
    return json.loads(cleaned)


def image_to_data_url(path):
    if not path or not os.path.exists(path):
        return ""
    ext = os.path.splitext(path)[1].lower().strip(".")
    mime = {
        "jpg": "image/jpeg",
        "jpeg": "image/jpeg",
        "png": "image/png",
        "webp": "image/webp",
    }.get(ext, "image/jpeg")
    with open(path, "rb") as handle:
        encoded = base64.b64encode(handle.read()).decode("ascii")
    return f"data:{mime};base64,{encoded}"


def build_analysis_from_model(payload, parsed, duration_ms, provider, model_name):
    record_id = payload.get("recordId") or f"rec_{uuid.uuid4().hex[:8]}"
    images = payload.get("images") or []
    point = payload.get("point") or {}
    observations = parsed.get("observations") or ["模型已返回结构化识别结果"]
    recognized = normalize_recognized_fields(parsed.get("recognizedFields") or [])
    recognition_status = parsed.get("recognitionStatus") or ("recognized" if recognized else "retake_required")
    retake_reason = parsed.get("retakeReason") or ("" if recognized else "模型未返回稳定字段")
    avg_confidence = average_confidence(recognized)

    result_images = []
    for idx, image in enumerate(images or [{"id": "img_demo"}], start=1):
        image_id = image.get("imageId") or image.get("id") or f"img_{idx:03d}"
        result_images.append({
            "imageId": image_id,
            "detectedImageType": "inspection_photo",
            "detectedScene": point.get("name", "巡检点位"),
            "observations": observations,
            "extractedFields": {},
            "visualFindings": [],
            "quality": {
                "isBlurry": recognition_status == "retake_required",
                "isOverExposed": False,
                "isRotated": False,
                "score": avg_confidence,
            },
            "confidence": avg_confidence,
            "needReview": True,
        })

    return {
        "schemaVersion": "1.0",
        "analysisId": f"ana_{uuid.uuid4().hex[:10]}",
        "recordId": record_id,
        "model": {
            "provider": provider,
            "name": model_name,
            "version": "openai-compatible",
        },
        "processedAt": now_iso(),
        "durationMs": duration_ms,
        "tokenUsage": {
            "input": 0,
            "output": 0,
            "imageCount": len(result_images),
        },
        "partialFailure": False,
        "failedImageIds": [],
        "images": result_images,
        "recognizedFields": recognized,
        "recognitionStatus": recognition_status,
        "retakeReason": retake_reason,
        "warnings": parsed.get("warnings") or [],
    }


def normalize_recognized_fields(fields):
    normalized = []
    for field in fields:
        if not isinstance(field, dict):
            continue
        code = str(field.get("code") or "").strip()
        value = str(field.get("value") or "").strip()
        if not code:
            continue
        try:
            confidence = float(field.get("confidence", 0.75))
        except (TypeError, ValueError):
            confidence = 0.75
        normalized.append({
            "code": code,
            "label": str(field.get("label") or ""),
            "value": value,
            "confidence": max(0.0, min(confidence, 1.0)),
            "reason": str(field.get("reason") or "千问视觉模型识别"),
        })
    return normalized


def average_confidence(fields):
    if not fields:
        return 0.35
    return round(sum(field.get("confidence", 0.75) for field in fields) / len(fields), 3)


def build_recognized_fields(fields, payload):
    point = payload.get("point") or {}
    now_text = datetime.now().strftime("%Y/%m/%d %H:%M")
    recognized = []
    for field in fields:
        code = field.get("code", "")
        label = field.get("label", "")
        kind = field.get("kind", "text")
        options = field.get("options") or []
        value = ""

        if code == "inspection_time":
            value = now_text
        elif code == "inspector":
            value = "巡检员"
        elif code == "asset_no":
            value = infer_asset_no(point.get("name", ""))
        elif code in ("site", "location"):
            value = point.get("project") or point.get("name") or "现场点位"
        elif "异常说明" in label or code in ("nonconformity", "exception_note"):
            value = "无"
        elif kind == "choice":
            value = default_choice(label, options)
        elif kind == "number":
            value = default_number(code, label)
        else:
            value = "正常" if "情况" in label else ""

        recognized.append({
            "code": code,
            "label": label,
            "value": value,
            "confidence": confidence_for(kind, value),
            "reason": "本地MockProvider根据表单字段生成，真实环境由千问视觉模型返回"
        })
    return recognized


def infer_asset_no(point_name):
    if "UPS" in point_name:
        return "1"
    if "变电" in point_name:
        return "1"
    if "电梯" in point_name:
        return "05"
    return point_name or "单点"


def default_choice(label, options):
    if not options:
        return "正常"
    if "报警" in label or "异常" in label or "异味" in label or "异响" in label:
        for expected in ("否", "无", "正常", "是"):
            if expected in options:
                return expected
    return options[0]


def default_number(code, label):
    mapping = {
        "tank_level": "3.0",
        "water_pressure": "0.45",
        "cabinet_temperature": "45",
        "battery_voltage": "542",
        "output_current": "0.36",
        "output_voltage": "220",
        "input_voltage": "220",
        "z1_reading": "1286.5",
        "z2_reading": "986.2",
        "z3_reading": "764.8",
        "z4_reading": "521.0",
        "living_water_reading": "305.6",
        "fire_water_reading": "118.3",
    }
    return mapping.get(code, "0")


def confidence_for(kind, value):
    if not value:
        return 0.0
    return 0.78 if kind == "number" else 0.86


class Handler(BaseHTTPRequestHandler):
    server_version = "InspectAIService/0.1"

    def log_message(self, fmt, *args):
        print("%s - %s" % (self.address_string(), fmt % args))

    def do_GET(self):
        if self.path == "/health":
            write_json(self, 200, {
                "status": "ok",
                "service": "ai-service",
                "demoMode": os.environ.get("DEMO_MODE", "true"),
                "provider": os.environ.get("AI_PROVIDER", "mock")
            })
            return
        write_json(self, 404, {"error": "not_found"})

    def do_POST(self):
        if self.path != "/analyze":
            write_json(self, 404, {"error": "not_found"})
            return

        try:
            payload = read_json(self)
            provider = os.environ.get("AI_PROVIDER", "mock").lower()
            if provider == "qwen":
                try:
                    write_json(self, 200, analyze_with_qwen(payload))
                    return
                except Exception as exc:
                    result = mock_analysis(payload)
                    result.setdefault("warnings", []).append(f"QWEN_CALL_FAILED: {exc}")
                    write_json(self, 200, result)
                    return
            time.sleep(0.4)
            write_json(self, 200, mock_analysis(payload))
        except Exception as exc:
            write_json(self, 500, {"error": "analysis_failed", "message": str(exc)})


if __name__ == "__main__":
    host = os.environ.get("AI_SERVICE_ADDR", "127.0.0.1")
    port = int(os.environ.get("AI_SERVICE_PORT", "9100"))
    httpd = ThreadingHTTPServer((host, port), Handler)
    print(f"AI service listening on http://{host}:{port}")
    httpd.serve_forever()
