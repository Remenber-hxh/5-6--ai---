#!/usr/bin/env python3
"""从 Noto Sans SC 变体字体切出本项目自带的字体子集。

用法:
    py -3 scripts/build-fonts.py [源字体路径]

源字体默认取 C:\\Windows\\Fonts\\NotoSansSC-VF.ttf(Win11 自带)。
换机器时可从 https://github.com/notofonts/noto-cjk 取同名文件。
授权是 SIL Open Font License 1.1 —— 允许嵌入、子集化、随产品分发,
条件是附带授权文本(见 public/fonts/OFL.txt)。


== 为什么要自己切 ==

中文全字集 17MB,整包塞给现场手机不可接受。但切成两片就不一样了:

  latin  拉丁字母 + 数字 + 标点     几十 KB,秒到
  cjk    汉字                      约 1.3MB,慢一点无所谓

分开的好处是:设备编号 K07、时间 2026-08-02 22:06、置信度百分比这些
【数字和字母】几乎立刻就是正确字体,而汉字在后面补上。这个 app 满屏是
编号和时间,先到的正好是占比最大的部分。

汉字片收哪些字:
  - 本应用真实会显示的字(界面文案 + 线上业务数据里扫出来的)
  - 并上 GB2312 一级字表 3755 字,覆盖现代中文约 99.7%
不在里面的生僻字回落到系统字体 —— 在这个领域(设备名、点位、巡检结论)
几乎遇不到。


== 为什么用可变字体而不是几个静态字重 ==

实测(WOFF):
    应用用字 · 整条字重轴          449 KB   400/500/600/700 全是真字重
    应用用字 · 静态单字重          254 KB   只有一个字重
    应用+常用3755 · 整条轴        1374 KB
    应用+常用3755 · 静态 400+600  1554 KB   两个文件,还更大

需要两个以上字重时,可变字体反而更省。而且它是连续轴,不会出现
"这个字重没有真字面,浏览器给你合成一个糊的" —— 那正是之前
微软雅黑上 500/600 看着不统一的原因。
"""
import io
import os
import re
import sys

from fontTools import subset
from fontTools.ttLib import TTFont

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
# 放 src/assets 而不是 public:vite.config 的 base 是 "./"(要能挂在 /v2/ 这类
# 子路径下)。public/ 里的文件只能用绝对路径引,子路径部署时就 404;
# 走 src/assets 由 Vite 改写 URL,才会跟着 base 走。
OUT = os.path.join(ROOT, "src", "assets", "fonts")
DEFAULT_SRC = r"C:\Windows\Fonts\NotoSansSC-VF.ttf"

CJK_RE = re.compile(r"[\u4e00-\u9fff]")


def gb2312_level1():
    """GB2312 一级汉字 3755 个。

    用编码枚举而不是外挂字表:一级字在 16-55 区,每区 94 位,
    双字节 (0xA0+区, 0xA0+位)。Python 自带 gb2312 编解码器,
    所以这份"常用字表"不需要任何外部文件,换机器也能复现。
    """
    out = set()
    for zone in range(16, 56):
        for pos in range(1, 95):
            try:
                out.add(bytes([0xA0 + zone, 0xA0 + pos]).decode("gb2312"))
            except UnicodeDecodeError:
                pass
    return out


def app_chars():
    """扫源码里的静态中文 —— 界面上写死的文案都在这儿。"""
    found = set()
    for base, dirs, files in os.walk(os.path.join(ROOT, "src")):
        dirs[:] = [d for d in dirs if d != "node_modules"]
        for f in files:
            if f.endswith((".ts", ".tsx", ".css")):
                text = io.open(os.path.join(base, f), encoding="utf-8", errors="ignore").read()
                found |= set(CJK_RE.findall(text))
    return found


def cut(src, name, codepoints):
    """切一片并存成 woff2。

    woff2 用 brotli 压缩,比 woff 的 zlib 小三成上下(实测汉字片
    1272KB → 883KB)。需要 `pip install brotli`,只装在构建机器上,
    不进产品。没装就退回 woff —— 少一个依赖比小三成更重要,
    别让换台机器就构建不出来。
    """
    font = TTFont(src, lazy=False)
    opts = subset.Options()
    opts.layout_features = ["*"]
    opts.name_IDs = ["*"]
    opts.name_legacy = True
    sub = subset.Subsetter(options=opts)
    sub.populate(unicodes=codepoints)
    sub.subset(font)
    try:
        import brotli  # noqa: F401

        ext = "woff2"
    except ImportError:
        ext = "woff"
        print("! 没装 brotli,退回 woff(体积大三成左右);装上可用:pip install brotli")
    font.flavor = ext
    path = os.path.join(OUT, name + "." + ext)
    font.save(path)
    return os.path.getsize(path), ext


def main():
    src = sys.argv[1] if len(sys.argv) > 1 else DEFAULT_SRC
    if not os.path.exists(src):
        sys.exit("找不到源字体:%s\n用法: py -3 scripts/build-fonts.py [源字体路径]" % src)
    os.makedirs(OUT, exist_ok=True)

    cmap = set(TTFont(src, lazy=True).getBestCmap())

    latin = {c for c in range(0x20, 0x7F)}
    latin |= {c for c in range(0xA0, 0x100)}          # 拉丁补充:°±×÷
    latin |= {c for c in range(0x2000, 0x2070)}       # 通用标点:— … ‧
    latin |= {c for c in range(0x3000, 0x3040)}       # 中文标点:、。《》
    latin |= {c for c in range(0xFF00, 0xFFF0)}       # 全角形式
    latin |= {ord(c) for c in "∅✦→↑↓✓"}              # 界面上用到的几个符号

    app = app_chars()
    cjk = {ord(c) for c in (app | gb2312_level1())}

    n_latin, ext = cut(src, "inspect-sans-latin", latin & cmap)
    n_cjk, _ = cut(src, "inspect-sans-cjk", cjk & cmap)

    print("源字体   %s  (%.1f MB)" % (os.path.basename(src), os.path.getsize(src) / 1048576))
    print("界面文案汉字 %d 个,并上 GB2312 一级表后共 %d 字" % (len(app), len(cjk)))
    print()
    print("  %-30s %7s" % ("产物", ext.upper()))
    print("  %-30s %6.0f KB" % ("inspect-sans-latin." + ext, n_latin / 1024))
    print("  %-30s %6.0f KB" % ("inspect-sans-cjk." + ext, n_cjk / 1024))
    print("  %-30s %6.0f KB" % ("合计", (n_latin + n_cjk) / 1024))
    print()
    print("产物扩展名变了的话,记得同步 src/styles/fonts.css 里的 url() 和 format()")


if __name__ == "__main__":
    main()
