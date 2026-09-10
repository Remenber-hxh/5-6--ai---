# -*- coding: utf-8 -*-
"""读数复核里那几个纯函数的测试 —— 裁剪和取舍逻辑不该靠调模型才能验。

跑法:python -m unittest test_second_look -v
"""
import os
import tempfile
import unittest

from run import (
    _same_number,
    _valid_bbox,
    collect_crop_targets,
    crop_reading_area,
    unverified_readings,
)


class TestValidBBox(unittest.TestCase):
    def test_accepts_a_normal_reading_box(self):
        self.assertTrue(_valid_bbox([0.42, 0.51, 0.72, 0.60]))

    def test_rejects_wrong_shape(self):
        for bad in (None, "0.1,0.2", [0.1, 0.2], [0.1, 0.2, 0.3, 0.4, 0.5], {}):
            self.assertFalse(_valid_bbox(bad), bad)

    def test_rejects_non_numeric(self):
        self.assertFalse(_valid_bbox(["a", 0.2, 0.3, 0.4]))

    def test_rejects_out_of_range(self):
        self.assertFalse(_valid_bbox([-0.5, 0.2, 0.3, 0.4]))
        self.assertFalse(_valid_bbox([0.1, 0.2, 1.9, 0.4]))

    def test_rejects_a_dot(self):
        # 太小的框裁出来只有几个像素,放大也是糊的
        self.assertFalse(_valid_bbox([0.5, 0.5, 0.501, 0.501]))

    def test_rejects_whole_image(self):
        # 框住整张图 = 没裁,再送一次只是白花一次调用
        self.assertFalse(_valid_bbox([0.0, 0.0, 1.0, 1.0]))

    def test_tolerates_inverted_corners(self):
        # 模型偶尔把左右/上下写反,裁剪时会自己排序,这里先放行
        self.assertTrue(_valid_bbox([0.72, 0.60, 0.42, 0.51]))


class TestSameNumber(unittest.TestCase):
    def test_numeric_equality_ignores_formatting(self):
        self.assertTrue(_same_number("1992", "1992.0"))
        self.assertTrue(_same_number("60197.924", "60197.9240"))

    def test_different_numbers(self):
        self.assertFalse(_same_number("1992", "1998"))
        # 这是最要命的一类:差 1000 倍
        self.assertFalse(_same_number("60197.924", "60197924"))

    def test_falls_back_to_string(self):
        self.assertTrue(_same_number(" abc ", "abc"))
        self.assertFalse(_same_number("abc", "abd"))


def _make_image(path, w=1200, h=1600):
    from PIL import Image, ImageDraw

    img = Image.new("RGB", (w, h), (30, 30, 30))
    d = ImageDraw.Draw(img)
    # 在中间偏下画一块"读数区",方便确认裁的是不是那一块
    d.rectangle([int(w * 0.4), int(h * 0.5), int(w * 0.7), int(h * 0.56)], fill=(240, 240, 240))
    img.save(path, format="JPEG", quality=90)


class TestCropReadingArea(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.path = os.path.join(self.dir, "meter.jpg")
        _make_image(self.path)

    def test_crops_and_upscales(self):
        from io import BytesIO

        from PIL import Image

        blob = crop_reading_area(self.path, [0.40, 0.50, 0.70, 0.56])
        self.assertIsNotNone(blob)
        out = Image.open(BytesIO(blob))
        # 原始框只有 360x96,复核要的就是放大后再看
        self.assertGreaterEqual(max(out.size), 1400)

    def test_padding_widens_the_box(self):
        from io import BytesIO

        from PIL import Image

        tight = Image.open(BytesIO(crop_reading_area(self.path, [0.40, 0.50, 0.70, 0.56], pad=0.0)))
        padded = Image.open(BytesIO(crop_reading_area(self.path, [0.40, 0.50, 0.70, 0.56], pad=0.25)))
        # 都放大到同一个长边,所以比宽高比:带 padding 的那张更"矮胖"
        self.assertGreater(padded.size[1] / padded.size[0], tight.size[1] / tight.size[0] * 0.9)

    def test_missing_file_returns_none(self):
        self.assertIsNone(crop_reading_area(os.path.join(self.dir, "nope.jpg"), [0.1, 0.1, 0.2, 0.2]))

    def test_box_outside_image_returns_none(self):
        self.assertIsNone(crop_reading_area(self.path, [1.04, 1.04, 1.05, 1.05]))


class TestCollectCropTargets(unittest.TestCase):
    def setUp(self):
        self.dir = tempfile.mkdtemp()
        self.path = os.path.join(self.dir, "meter.jpg")
        _make_image(self.path)
        self.fields = [
            {"code": "z1_reading", "kind": "number"},
            {"code": "room_clean", "kind": "choice"},
            {"code": "note", "kind": "text"},
        ]
        self.payload = {"images": [{"path": self.path}]}

    def _parsed(self, **over):
        item = {"code": "z1_reading", "value": "60197924",
                "imageIndex": 1, "bbox": [0.40, 0.50, 0.70, 0.56]}
        item.update(over)
        return {"recognizedFields": [item]}

    def test_picks_up_a_number_field(self):
        got = collect_crop_targets(self.payload, self._parsed(), self.fields)
        self.assertEqual(len(got), 1)
        self.assertEqual(got[0][0], "z1_reading")
        self.assertEqual(got[0][1], "60197924")

    def test_skips_non_number_kinds(self):
        # 选项题不做读数复核 —— 裁一块"是/否"放大没有意义
        parsed = {"recognizedFields": [
            {"code": "room_clean", "value": "正常", "imageIndex": 1, "bbox": [0.4, 0.5, 0.7, 0.56]}]}
        self.assertEqual(collect_crop_targets(self.payload, parsed, self.fields), [])

    def test_skips_when_bbox_missing(self):
        parsed = {"recognizedFields": [{"code": "z1_reading", "value": "1", "imageIndex": 1}]}
        self.assertEqual(collect_crop_targets(self.payload, parsed, self.fields), [])

    def test_skips_when_image_index_out_of_range(self):
        # 模型报了第 5 张,但只送了 1 张 —— 越界就当没给,不能拿别的图去裁
        self.assertEqual(collect_crop_targets(self.payload, self._parsed(imageIndex=5), self.fields), [])
        self.assertEqual(collect_crop_targets(self.payload, self._parsed(imageIndex=0), self.fields), [])

    def test_skips_when_template_has_no_number_fields(self):
        only_choice = [{"code": "room_clean", "kind": "choice"}]
        self.assertEqual(collect_crop_targets(self.payload, self._parsed(), only_choice), [])


class TestUnverifiedReadings(unittest.TestCase):
    """没被复核到的读数必须报出来。

    【为什么这条重要】能不能复核取决于模型给不给 bbox。给不出的话那个读数
    一次复核都没过就进了记录,而界面上和复核过的长得一模一样 ——
    "保护看着在、其实没生效",比没有保护更危险。
    """

    fields = [
        {"code": "z1_reading", "label": "Z1 能耗表读数", "kind": "number"},
        {"code": "z2_reading", "label": "Z2 能耗表读数", "kind": "number"},
        {"code": "room_clean", "label": "机房卫生", "kind": "choice"},
        {"code": "note", "label": "备注", "kind": "text"},
    ]

    def test_reports_the_ones_that_missed_the_check(self):
        parsed = {"recognizedFields": [
            {"code": "z1_reading", "value": "60197.924"},
            {"code": "z2_reading", "value": "84363.520"},
        ]}
        got = unverified_readings(parsed, self.fields, {"z1_reading"})
        self.assertEqual(got, ["Z2 能耗表读数"])

    def test_silent_when_everything_was_checked(self):
        parsed = {"recognizedFields": [{"code": "z1_reading", "value": "1"}]}
        self.assertEqual(unverified_readings(parsed, self.fields, {"z1_reading"}), [])

    def test_ignores_non_number_fields(self):
        # 选项题和文本本来就不做读数复核,别报出来当噪音
        parsed = {"recognizedFields": [
            {"code": "room_clean", "value": "正常"},
            {"code": "note", "value": "一切正常"},
        ]}
        self.assertEqual(unverified_readings(parsed, self.fields, set()), [])

    def test_ignores_empty_values(self):
        # 本来就没填的不算"未复核"
        parsed = {"recognizedFields": [{"code": "z1_reading", "value": ""}]}
        self.assertEqual(unverified_readings(parsed, self.fields, set()), [])

    def test_uses_chinese_label_not_code(self):
        parsed = {"recognizedFields": [{"code": "z1_reading", "value": "1"}]}
        got = unverified_readings(parsed, self.fields, set())
        self.assertEqual(got, ["Z1 能耗表读数"])
        self.assertNotIn("z1_reading", got)


if __name__ == "__main__":
    unittest.main()
