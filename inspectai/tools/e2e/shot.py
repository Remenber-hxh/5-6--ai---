# -*- coding: utf-8 -*-
"""登录 admin-web 并截指定路由(自查/发版冒烟用)。

用法:
    python tools/e2e/shot.py [route] [out.png] [wait_ms]
环境变量:
    E2E_BASE  前端地址(默认 http://localhost:18090)
    E2E_API   后端地址(默认 http://localhost:18080)
    E2E_USER / E2E_PASS  登录账号(默认 admin / 本地默认密码)
"""
import json
import os
import sys
import urllib.request

from playwright.sync_api import sync_playwright

BASE = os.environ.get("E2E_BASE", "http://localhost:18090")
API = os.environ.get("E2E_API", "http://localhost:18080")
USER = os.environ.get("E2E_USER", "admin")
PASS = os.environ.get("E2E_PASS", "InspectAI@2026")

route = sys.argv[1] if len(sys.argv) > 1 else "/"
out = sys.argv[2] if len(sys.argv) > 2 else "tools/e2e/out/shot.png"
wait_ms = int(sys.argv[3]) if len(sys.argv) > 3 else 4000

os.makedirs(os.path.dirname(out) or ".", exist_ok=True)

req = urllib.request.Request(
    API + "/api/auth/login",
    data=json.dumps({"username": USER, "password": PASS}).encode(),
    headers={"Content-Type": "application/json"},
)
d = json.loads(urllib.request.urlopen(req, timeout=15).read())
token = d["token"]
user = json.dumps(d.get("user") or {}, ensure_ascii=False)

with sync_playwright() as p:
    b = p.chromium.launch(channel="chrome")
    ctx = b.new_context(viewport={"width": 1600, "height": 900})
    # 页面脚本运行前注入登录态,避免被重定向到登录页
    ctx.add_init_script(
        "localStorage.setItem('inspectai_admin_token', %s);"
        "localStorage.setItem('inspectai_admin_user', %s);"
        "localStorage.setItem('inspectai_live2d', 'off');" % (json.dumps(token), json.dumps(user))
    )
    pg = ctx.new_page()
    pg.goto(BASE + "/#" + route, wait_until="domcontentloaded")
    pg.wait_for_timeout(wait_ms)
    pg.screenshot(path=out)
    b.close()
print("SHOT_OK", out)
