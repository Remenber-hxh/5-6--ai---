"""AI 总结的拼装规则。

跑法(不需要装任何东西):
    python ai-service/test_summary.py

【为什么这块必须有测试】这段话是领导唯一会读的东西 —— 他不会点开表单
逐字段核对。所以它错了没人发现,而且错法很隐蔽:数字对不上、字段代码
漏出去、把"照片没拍到"说成"设备坏了"。每一种都不报错、界面照常。
"""

import importlib.util
import os
import sys

sys.stdout.reconfigure(encoding="utf-8")

_spec = importlib.util.spec_from_file_location(
    "run", os.path.join(os.path.dirname(os.path.abspath(__file__)), "run.py")
)
run = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(run)

FAILED = []


def check(name, cond, detail=""):
    if cond:
        print(f"PASS  {name}")
    else:
        print(f"FAIL  {name}" + (f"\n      {detail}" if detail else ""))
        FAILED.append(name)


# 截图里那条真实记录(2026-09-01 KT-6 号电梯)
REAL = {
    "templateName": "电梯巡检（无机房）",
    "project": "会议中心",
    "pointName": "无机房电梯",
    "inspector": "朱佳伟",
    "inspectionTime": "9月1日 13:23",
    "fields": [
        {"code": "reg_mark", "label": "电梯使用登记标志完好", "value": "否"},
        {"code": "alarm_device", "label": "紧急报警装置有效", "value": "否"},
        {"code": "floor_buttons", "label": "选层按钮及显示正常", "value": "否"},
        {"code": "door_protect", "label": "轿门防夹装置正常", "value": "是"},
        {"code": "door_run", "label": "开关门运行正常", "value": "是"},
        {"code": "lighting", "label": "轿厢照明正常", "value": "是"},
        {"code": "fire_switch_glass", "label": "消防开关玻璃完好", "value": "是"},
        {
            "code": "nonconformity",
            "label": "不符合项处理情况记录",
            "value": "reg_mark:缺失使用标志; alarm_device:未见报警装置; floor_buttons:未拍操作面板",
        },
    ],
}

bad, gaps = run.summarize_field_findings(REAL)
head = run.enforce_summary_policy("", REAL)

# ===== 1. 记录字段不能被当成一个问题 =====
# 【原来的 bug】"不符合项处理情况记录"的正文里带着"缺失"两个字,被算成第 4 项,
# 于是开头写"待跟进 4 项"、正文写"发现 3 项不符合项",同一段话自相矛盾。
check("记录字段不算一个问题", len(bad) == 2, f"实际 {len(bad)} 项: {bad}")
check(
    "不符合项处理情况记录 本身不出现在清单里",
    not any("处理情况记录" in b for b in bad),
    str(bad),
)

# ===== 2. 字段代码绝不能漏给用户 =====
for code in ("reg_mark", "alarm_device", "floor_buttons", "nonconformity"):
    check(f"总结里不出现字段代码 {code}", code not in head, head)

# ===== 3. 照片没拍到 ≠ 设备坏了 =====
# 模板提示词里明写着"缺照片 ≠ 设备异常";混在一起会把问题数量报高。
check("未拍到的项单独归类", gaps == ["未拍操作面板"], str(gaps))
check("未拍到的项不算不符合", not any("未拍" in b for b in bad), str(bad))
check("开头把两类分开说", "无法判定" in head, head)

# ===== 4. 不能出现表单原文 =====
check("不出现 `字段=值` 这种表单写法", "=否" not in head and "=是" not in head, head)

# ===== 5. 数字只报一次,且和清单一致 =====
check("开头报的数与清单条数一致", f"{len(bad)} 项不符合" in head, head)
check("不再使用会和正文打架的旧措辞", "待跟进" not in head, head)

# ===== 6. 拆不出 code:描述 时要退回人话标签 =====
PROSE = {
    "templateName": "电梯巡检",
    "inspector": "张三",
    "inspectionTime": "9月1日 10:00",
    "fields": [
        {"code": "reg_mark", "label": "电梯使用登记标志完好", "value": "否"},
        {"code": "alarm_device", "label": "紧急报警装置有效", "value": "否"},
        {
            "code": "nonconformity",
            "label": "不符合项处理情况记录",
            "value": "登记标志已过期，报警装置按下无响应。",  # 模型写成整句,拆不出 code
        },
    ],
}
bad2, _ = run.summarize_field_findings(PROSE)
check("模型写成整句时仍能出 2 项", len(bad2) == 2, str(bad2))
check(
    "退回时说人话而不是 `完好=否`",
    "电梯使用登记标志不完好" in bad2 and "紧急报警装置失效" in bad2,
    str(bad2),
)

# ===== 7. 全部正常时不要凭空造问题 =====
CLEAN = {**REAL, "fields": [f for f in REAL["fields"] if f.get("value") == "是"]}
bad3, gaps3 = run.summarize_field_findings(CLEAN)
check("全部正常时没有不符合项", bad3 == [] and gaps3 == [], f"{bad3} / {gaps3}")
check(
    "全部正常时开头不加任何前缀",
    run.enforce_summary_policy("各项均正常。", CLEAN) == "各项均正常。",
    run.enforce_summary_policy("各项均正常。", CLEAN),
)

# ===== 8. 反向问法不能翻过来 =====
# 【最容易写错的一条】"设备无异响、异味"带着"异响"二字,只按关键词匹配
# 会把它当成反向问法,于是「否」被判成正常 —— 语义正好翻了过来。
REVERSE = {
    "templateName": "综合巡检",
    "inspector": "李四",
    "inspectionTime": "9月1日 11:00",
    "fields": [
        {"code": "no_noise", "label": "设备无异响、异味", "value": "否"},
        {"code": "leak", "label": "是否漏水", "value": "是"},
        {"code": "normal_run", "label": "运行正常", "value": "是"},
    ],
}
bad4, _ = run.summarize_field_findings(REVERSE)
check("「设备无异响」判否 = 异常", any("异响" in b for b in bad4), str(bad4))
check("「是否漏水」判是 = 异常", any("漏水" in b for b in bad4), str(bad4))
check("「运行正常」判是 不算异常", not any("运行正常" in b for b in bad4), str(bad4))

# ===== 9. 兜底正文也不许倒字段 =====
check(
    "兜底正文不出现 `字段=值`",
    "=" not in run.enforce_summary_policy("", CLEAN),
    run.enforce_summary_policy("", CLEAN),
)

print()
print("最终这条记录的总结:")
print(" ", run.enforce_summary_policy(
    "9月1日13:23，会议中心无机房电梯点位由朱佳伟完成巡检，本次检查电梯编号KT-6。"
    "其余各项（轿门防夹、开关门运行、照明、消防开关玻璃）均正常。", REAL))
print()
if FAILED:
    print(f"{len(FAILED)} 条失败:", "、".join(FAILED))
    sys.exit(1)
print("全部通过")
