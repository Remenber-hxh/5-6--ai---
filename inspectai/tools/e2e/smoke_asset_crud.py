# -*- coding: utf-8 -*-
"""资产台账 CRUD 冒烟:UI 新增 → 列表可见 → 右上角菜单删除 → 无残留。

前置:本地后端(scripts/start-local.ps1)与 admin-web dev(18090)已启动。
环境变量同 shot.py(E2E_BASE / E2E_API / E2E_USER / E2E_PASS)。
退出码 0 = 通过。
"""
import json
import os
import sys
import urllib.request

from playwright.sync_api import sync_playwright

sys.stdout.reconfigure(encoding="utf-8", errors="replace")

BASE = os.environ.get("E2E_BASE", "http://localhost:18090")
API = os.environ.get("E2E_API", "http://localhost:18080")
USER = os.environ.get("E2E_USER", "admin")
PASS = os.environ.get("E2E_PASS", "InspectAI@2026")
MARK = "冒烟测试设备-勿动"

req = urllib.request.Request(
    API + "/api/auth/login",
    data=json.dumps({"username": USER, "password": PASS}).encode(),
    headers={"Content-Type": "application/json"},
)
d = json.loads(urllib.request.urlopen(req, timeout=15).read())
token, user = d["token"], json.dumps(d.get("user") or {}, ensure_ascii=False)

failed = []
with sync_playwright() as p:
    b = p.chromium.launch(channel="chrome")
    ctx = b.new_context(viewport={"width": 1600, "height": 900})
    ctx.add_init_script(
        "localStorage.setItem('inspectai_admin_token', %s);"
        "localStorage.setItem('inspectai_admin_user', %s);"
        "localStorage.setItem('inspectai_live2d','off');" % (json.dumps(token), json.dumps(user))
    )
    pg = ctx.new_page()
    pg.goto(BASE + "/", wait_until="domcontentloaded")
    pg.wait_for_timeout(1500)
    pg.get_by_text("资产台账", exact=True).first.click()
    pg.wait_for_timeout(3000)

    # 新增
    pg.get_by_role("button", name="新增资产").click()
    pg.wait_for_timeout(500)
    pg.locator("#project").fill("会议中心")
    pg.locator("#assetKey").fill("SMOKE-1")
    pg.locator("#assetName").fill(MARK)
    pg.locator(".ant-modal .ant-btn-primary").click()
    pg.wait_for_timeout(2500)
    if pg.get_by_text(MARK).count() < 1:
        failed.append("create: 新增后列表未出现")

    # 删除(详情右上角菜单)
    pg.locator(".ant-card-extra button").last.click()
    pg.wait_for_timeout(500)
    pg.get_by_text("删除资产").click()
    pg.wait_for_timeout(500)
    pg.locator(".ant-modal-confirm .ant-btn-dangerous").click()
    pg.wait_for_timeout(2000)
    if pg.get_by_text(MARK).count() != 0:
        failed.append("delete: 删除后仍有残留")
    b.close()

if failed:
    print("SMOKE FAIL:", "; ".join(failed))
    sys.exit(1)
print("SMOKE PASS: asset create/delete")
