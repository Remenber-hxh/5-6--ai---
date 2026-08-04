# 自带字体

| | |
|---|---|
| 字体 | Noto Sans SC(思源黑体简体中文)可变字体 |
| 版本 | 2.04 |
| 作者 | Adobe / Google |
| 授权 | **SIL Open Font License 1.1** |
| 授权原文 | <http://scripts.sil.org/OFL> |
| 上游 | <https://github.com/notofonts/noto-cjk> |

`inspect-sans-latin.woff` / `inspect-sans-cjk.woff` 是从
`NotoSansSC-VF.ttf` 切出来的子集,生成方式见 `../../../scripts/build-fonts.py`。
换机器或要改收字范围时重跑那个脚本即可,不要手工替换这两个文件。

## 为什么选它

选型时比过 MiSans(小米)、HarmonyOS Sans(华为)、思源黑体。三者都免费
商用,观感上 MiSans 和 HarmonyOS Sans 更现代一些,但它们是厂商自定授权,
条款里对再分发有额外约定。**OFL 1.1 明确允许嵌入、子集化、随产品分发**,
对我们要走软著和商业交付的情形最干净,所以选思源。

## OFL 合规待办

OFL 要求分发时附带授权原文。目前仓库里**还没有** `OFL.txt` ——
这台机器上找不到现成副本,而联网取文件需要先经确认。

**上线前必须补上**:从 <http://scripts.sil.org/OFL> 取 OFL 1.1 全文,
存为本目录下的 `OFL.txt`。字体文件内部的 name 表(nameID 13/14)已经带了
授权声明和链接,但那只是声明,不能替代随附原文。
