// 设备二维码格式的自检。
//
// 这段逻辑错了的后果很特别:码印出来了、贴上去了,到现场才发现扫不出东西——
// 而那时候贴纸已经贴了几百张。所以它值得有个能反复跑的校验。
//
// 跑法:npm run check:qr

import {
  buildScanURL,
  parseScannedText,
  splitAssetID,
} from "../src/lib/assetQR";

let failed = 0;

function ok(cond: boolean, what: string) {
  if (!cond) {
    failed++;
    console.log("  FAIL  " + what);
  }
}

function eq(got: unknown, want: unknown, what: string) {
  const g = JSON.stringify(got);
  const w = JSON.stringify(want);
  if (g !== w) {
    failed++;
    console.log(`  FAIL  ${what}\n        得到 ${g}\n        期望 ${w}`);
  }
}

// ── 拆 ID ────────────────────────────────────────────
const t = splitAssetID("会议中心::elevator_no_room::KT-7");
eq(t?.project, "会议中心", "项目名");
eq(t?.templateId, "elevator_no_room", "模板");
eq(t?.assetNo, "KT-7", "设备编号");

// 【编号里带 :: 的历史数据】只按前两个分隔符切,剩下的整体是编号。
// 切多了会把编号截断,截断后的编号匹配不上任何设备 —— 现场表现是"扫了没反应"。
eq(splitAssetID("会议中心::power_room::A::B")?.assetNo, "A::B", "编号里含 :: 不能被截断");

// 缺段的一律不认,别猜
for (const bad of ["", "  ", "只有名字", "a::b", "::b::c", "a::::c", "a::b::"]) {
  ok(splitAssetID(bad) === null, `残缺 ID 应拒绝:${JSON.stringify(bad)}`);
}

// ── 扫到的文本 ───────────────────────────────────────
const url = buildScanURL("会议中心::elevator_no_room::KT-7", "https://inspect.example.com");
eq(url, "https://inspect.example.com/#/scan?a=" +
  encodeURIComponent("会议中心::elevator_no_room::KT-7"), "生成的二维码内容");

// 三种写法都要认出同一台设备
for (const raw of [
  url,                                        // 完整 URL(相机/微信扫到的)
  "#/scan?a=" + encodeURIComponent("会议中心::elevator_no_room::KT-7"), // 只有 hash
  "会议中心::elevator_no_room::KT-7",           // 裸 ID(手抄或旧批次)
]) {
  eq(parseScannedText(raw)?.assetId, "会议中心::elevator_no_room::KT-7",
    `应认出:${raw.slice(0, 40)}`);
}

// 带多余参数也要能取到
eq(parseScannedText(url + "&from=wechat")?.assetNo, "KT-7", "URL 带其他参数");

// 【别人家的码要老实说不认识】认得太宽会让巡检员扫任何东西都"成功",
// 然后落到一台莫名其妙的设备上 —— 比扫不出来严重得多。
for (const junk of [
  "https://weixin.qq.com/g/abcdef",
  "BEGIN:VCARD\nFN:张三\nEND:VCARD",
  "https://inspect.example.com/#/scan?a=",     // 参数空
  "https://inspect.example.com/#/scan?a=%E4%B8%8D%E5%AE%8C%E6%95%B4", // 不是资产 ID
  "12345678",
]) {
  ok(parseScannedText(junk) === null, `不该认:${junk.slice(0, 36)}`);
}

// origin 传空时退回相对形式(只能被 app 自己扫)
ok(buildScanURL("a::b::c", "").startsWith("#/scan?a="), "空 origin 退回相对形式");
// 结尾多余的斜杠不该产生 //
ok(!buildScanURL("a::b::c", "https://x.com/").includes("com//"), "origin 结尾斜杠要归一");

if (failed) {
  console.log(`\n  ${failed} 项未通过`);
  process.exit(1);
}
console.log("  二维码格式自检通过");
