# -*- coding: utf-8 -*-
"""模型调用公共层的测试:密钥不进命令行、守住时间预算、业务错误不重试。

跑法:python -m unittest test_ai_call -v
"""
import time
import unittest
from unittest import mock

import run


class TestKeyNotInArgv(unittest.TestCase):
    def test_curl_args_reference_a_header_file_not_the_key(self):
        args = run.curl_args("https://x/chat/completions", "/tmp/h.hdr", "/tmp/b.json", 30)
        joined = " ".join(args)
        self.assertNotIn("Bearer", joined)
        self.assertIn("@/tmp/h.hdr", args)

    def test_the_key_goes_into_the_header_file(self):
        seen = {}

        def fake_run(args, **kw):
            hdr = args[args.index("-H") + 1].lstrip("@")
            with open(hdr, encoding="utf-8") as fp:
                seen["header"] = fp.read()
            seen["argv"] = " ".join(args)
            return mock.Mock(returncode=0, stdout=b'{"choices":[{"message":{"content":"hi"}}]}', stderr=b"")

        with mock.patch.object(run.subprocess, "run", side_effect=fake_run):
            out = run._curl_post_once("https://x", b"{}", "sk-secret-123", 10)
        self.assertEqual(out["choices"][0]["message"]["content"], "hi")
        self.assertIn("Bearer sk-secret-123", seen["header"])
        self.assertNotIn("sk-secret-123", seen["argv"])


OK_PAYLOAD = {"choices": [{"message": {"content": "ok"}}], "usage": {"prompt_tokens": 1}}


class TestBudgetAndRetry(unittest.TestCase):
    def call(self, **kw):
        base = dict(provider="qwen", url="https://x", body={"model": "m"}, api_key="k", timeout=30)
        base.update(kw)
        return run.post_chat_completion(**base)

    def test_retries_a_network_failure(self):
        calls = [run.AICallError("curl exit 28", retryable=True), OK_PAYLOAD]

        def fake(*a, **kw):
            r = calls.pop(0)
            if isinstance(r, Exception):
                raise r
            return r

        with mock.patch.object(run, "_curl_post_once", side_effect=fake), \
                mock.patch.object(run.time, "sleep"):
            self.assertEqual(self.call(max_retries=1), OK_PAYLOAD)

    def test_business_error_is_not_retried(self):
        fake = mock.Mock(return_value={"error": {"code": "Arrearage", "message": "欠费"}})
        with mock.patch.object(run, "_curl_post_once", fake):
            with self.assertRaises(run.AICallError) as ctx:
                self.call(max_retries=3)
        self.assertEqual(fake.call_count, 1)
        self.assertIn("Arrearage", str(ctx.exception))  # 账号故障识别靠这个码

    def test_no_request_when_budget_is_gone(self):
        fake = mock.Mock(return_value=OK_PAYLOAD)
        with mock.patch.object(run, "_curl_post_once", fake):
            with self.assertRaises(run.AICallError):
                self.call(deadline=time.time() + 1)
        fake.assert_not_called()

    def test_attempt_timeout_is_capped_by_the_budget(self):
        fake = mock.Mock(return_value=OK_PAYLOAD)
        with mock.patch.object(run, "_curl_post_once", fake):
            self.call(timeout=90, deadline=time.time() + 20)
        used_timeout = fake.call_args[0][3]
        self.assertLessEqual(used_timeout, 20)

    def test_no_retry_when_budget_runs_out(self):
        fake = mock.Mock(side_effect=run.AICallError("timeout", retryable=True))
        with mock.patch.object(run, "_curl_post_once", fake), \
                mock.patch.object(run, "seconds_left", side_effect=[30, 30, 1]):
            with self.assertRaises(run.AICallError):
                self.call(max_retries=3, deadline=time.time() + 30)
        self.assertEqual(fake.call_count, 1)


class TestBudgetDeadline(unittest.TestCase):
    def test_uses_caller_budget(self):
        d = run.budget_deadline({"budgetSeconds": 12}, 75)
        self.assertAlmostEqual(d - time.time(), 12, delta=1)

    def test_falls_back_to_default_on_garbage(self):
        for bad in ({}, {"budgetSeconds": "x"}, {"budgetSeconds": -5}):
            d = run.budget_deadline(bad, 75)
            self.assertAlmostEqual(d - time.time(), 75, delta=1)


class TestClassifyNeedsManual(unittest.TestCase):
    def test_low_confidence_needs_manual_even_if_model_says_no(self):
        self.assertTrue(run.classify_needs_manual("zihan_energy", 0.3, False))

    def test_unknown_needs_manual(self):
        self.assertTrue(run.classify_needs_manual("unknown", 0.95, False))

    def test_confident_and_known_is_automatic(self):
        self.assertFalse(run.classify_needs_manual("zihan_energy", 0.9, False))

    def test_model_can_still_ask_for_manual(self):
        self.assertTrue(run.classify_needs_manual("zihan_energy", 0.9, True))


class TestSecondLookRespectsBudget(unittest.TestCase):
    def test_skips_and_warns_when_time_is_short(self):
        parsed = {"recognizedFields": [{"code": "z1", "value": "123"}]}
        with mock.patch.object(run, "collect_crop_targets", return_value=[("z1", "123", b"x")]), \
                mock.patch.object(run, "call_qwen_chat") as qwen:
            out = run.second_look({}, parsed, [{"code": "z1"}], "k", deadline=time.time() + 5)
        qwen.assert_not_called()
        self.assertTrue(any("未做放大复核" in w for w in out.get("warnings", [])))


if __name__ == "__main__":
    unittest.main()
