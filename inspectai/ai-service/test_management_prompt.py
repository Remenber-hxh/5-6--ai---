# -*- coding: utf-8 -*-
"""管理聊天备用路的提示词:共用规矩用后端发来的那一份,自己只留看板数据的说明。

跑法:python -m unittest test_management_prompt -v
"""
import unittest
from unittest import mock

import run


class TestManagementChatPrompt(unittest.TestCase):
    def test_uses_rules_sent_by_backend(self):
        system = run.management_chat_system({"sharedRules": "【后端发来的规矩】数字不能编"})
        self.assertIn("topRiskAssets", system)  # 看板数据的说明还在
        self.assertTrue(system.endswith("【后端发来的规矩】数字不能编"))

    def test_falls_back_to_minimal_rules(self):
        system = run.management_chat_system({})
        self.assertIn(run.MANAGEMENT_CHAT_RULES_FALLBACK, system)

    def test_no_second_full_copy_here(self):
        # 动作提议、输出字数这些只在后端写一份;这里再出现就是又抄了一份
        for marker in ("<<ACTION>>", "create_recheck_task", "50-90 字", "50-120 字"):
            self.assertNotIn(marker, run.MANAGEMENT_CHAT_CONTEXT)
            self.assertNotIn(marker, run.MANAGEMENT_CHAT_RULES_FALLBACK)

    def test_chat_sends_backend_rules_to_model(self):
        seen = {}

        def fake_chat(**kw):
            seen["system"] = kw["system"]
            return "好的", "deepseek-chat"

        with mock.patch.object(run, "get_deepseek_key", return_value="k"), \
                mock.patch.object(run, "call_deepseek_chat", side_effect=fake_chat):
            run.management_chat({"message": "今天重点关注什么", "sharedRules": "【规矩】RULES-XYZ"})
        self.assertTrue(seen["system"].endswith("【规矩】RULES-XYZ"))


if __name__ == "__main__":
    unittest.main()
