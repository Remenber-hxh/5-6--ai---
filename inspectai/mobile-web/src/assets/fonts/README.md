# 自带字体

| | |
|---|---|
| 字体 | Noto Sans SC(思源黑体简体中文)可变字体 |
| 版本 | 2.04 |
| 作者 | Adobe / Google |
| 授权 | **SIL Open Font License 1.1** |
| 授权原文 | <http://scripts.sil.org/OFL> |
| 上游 | <https://github.com/notofonts/noto-cjk> |

`inspect-sans-latin.woff2` / `inspect-sans-cjk.woff2` 是从
`NotoSansSC-VF.ttf` 切出来的子集,生成方式见 `../../../scripts/build-fonts.py`。
换机器或要改收字范围时重跑那个脚本即可,不要手工替换这两个文件。

构建机器需要 `pip install fonttools brotli`。没装 brotli 时脚本会自动退回
woff(大三成左右),并在输出里提示 —— 换台机器也构建得出来,只是文件大些。
产物扩展名变了要同步改 `src/styles/fonts.css` 的 `url()` 和 `format()`。

## 为什么选它

选型时比过 MiSans(小米)、HarmonyOS Sans(华为)、思源黑体。三者都免费
商用,观感上 MiSans 和 HarmonyOS Sans 更现代一些,但它们是厂商自定授权,
条款里对再分发有额外约定。**OFL 1.1 明确允许嵌入、子集化、随产品分发**,
对我们要走软著和商业交付的情形最干净,所以选思源。

## OFL 合规

- [x] `OFL.txt` 已随附(OFL 1.1 全文,取自 <https://openfontlicense.org/>,
      开头是本字体真实的版权行,与字体 name 表 nameID 0 一致)
- [x] 保留字名不冲突 —— 本字体的 Reserved Font Name 是 `'Source'`,
      我们的子集内部仍叫 `Noto Sans SC`,不含该保留名。
      (CSS 里那个 `InspectSans` 只是 `@font-face` 的对外名字,
      不改字体内部的 name 表,不涉及保留字名。)
- [x] 未单独售卖字体本身,仅作为产品的一部分分发 —— OFL 允许。

对外交付或提交软著时,把本目录连同 `OFL.txt` 一起带上即可。
