import { PrinterOutlined } from "@ant-design/icons";
import { Alert, Button, Input, Modal, Segmented, Space, Spin, Typography, message } from "antd";
import QRCode from "qrcode";
import { useEffect, useMemo, useState } from "react";

import { AssetEntry } from "../api/mgmt";
import { buildScanURL } from "../lib/assetQR";

/**
 * 设备二维码：生成 + 批量打印成贴纸。
 *
 * 一台设备一个码，贴在设备上。巡检员到现场扫一下，直接进入这台设备的巡检，
 * 不用再从列表里找、也不用靠 AI 从照片里认编号——认错编号会把记录落到别的设备上，
 * 这是台账对不上账的主要来源之一。
 *
 * 【纠错等级默认 M，不是最高的 H】看着反直觉，但小贴纸上格子大小比容损率更要命：
 * 同样 24mm，H 级每格只有 0.42mm、M 级有 0.59mm。机房光线差、手机要贴很近才对焦，
 * 格子太小根本扫不出来——而"扫不出来"是每天都会发生的，"贴纸破损 30%"不是。
 * 想要更强的抗损，把贴纸印大一点比调高纠错等级有效得多。
 */

/** 贴纸尺寸。数字是毫米，直接对应打印出来的实际大小。 */
const SIZES = {
  小: { qr: 24, w: 40, name: 9, code: 7.5 },
  中: { qr: 32, w: 52, name: 10.5, code: 8.5 },
  大: { qr: 44, w: 68, name: 13, code: 10 },
} as const;
type SizeKey = keyof typeof SIZES;

/** 手机能稳定扫出来的每格下限（毫米）。低于它就该换大贴纸或降纠错等级。 */
const MIN_MODULE_MM = 0.5;

const ECC = {
  标准: "M",
  较高: "Q",
  最高: "H",
} as const;
type EccKey = keyof typeof ECC;

export default function AssetQRSheet({
  assets,
  open,
  onClose,
}: {
  assets: AssetEntry[];
  open: boolean;
  onClose: () => void;
}) {
  // 移动端的访问地址。线上后台和移动端同域，所以默认取当前域名；
  // 本地开发是两个端口，必须能改——否则印出来的码指向后台自己，扫了打不开。
  const [origin, setOrigin] = useState(() => window.location.origin);
  const [size, setSize] = useState<SizeKey>("中");
  const [ecc, setEcc] = useState<EccKey>("标准");
  const [codes, setCodes] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);

  const spec = SIZES[size];
  const list = useMemo(() => assets.filter((a) => a.id), [assets]);

  // 每格多少毫米。按【最长的那个 ID】算——一批贴纸里只要有一张扫不出来就是返工，
  // 所以按最坏情况给结论，不按平均。
  const moduleMM = useMemo(() => {
    if (!list.length) return null;
    const longest = list.reduce((m, a) => (a.id.length > m.length ? a.id : m), list[0].id);
    try {
      const n = QRCode.create(buildScanURL(longest, origin), {
        errorCorrectionLevel: ECC[ecc],
      }).modules.size;
      return { mm: spec.qr / n, modules: n };
    } catch {
      return null;
    }
  }, [list, origin, ecc, spec.qr]);

  useEffect(() => {
    if (!open || !list.length) return;
    let alive = true;
    setBusy(true);
    (async () => {
      const next: Record<string, string> = {};
      for (const a of list) {
        try {
          next[a.id] = await QRCode.toDataURL(buildScanURL(a.id, origin), {
            errorCorrectionLevel: ECC[ecc],
            margin: 1,
            // 生成得比打印尺寸大，缩小显示才不会有锯齿；打印机分辨率远高于屏幕
            width: 480,
            color: { dark: "#000000", light: "#FFFFFF" },
          });
        } catch {
          // 单台失败不该让整页出不来——下面会显示成"生成失败"，至少看得见是哪一台
        }
      }
      if (!alive) return;
      setCodes(next);
      setBusy(false);
    })();
    return () => {
      alive = false;
    };
  }, [open, list, origin, ecc]);

  function doPrint() {
    const missing = list.filter((a) => !codes[a.id]).length;
    if (missing) {
      message.warning(`有 ${missing} 台的二维码没生成出来，请先检查再打印`);
      return;
    }
    window.print();
  }

  const tooDense = moduleMM !== null && moduleMM.mm < MIN_MODULE_MM;

  return (
    <>
      <Modal
        title="设备二维码"
        open={open}
        onCancel={onClose}
        width={920}
        destroyOnHidden
        footer={
          <Space>
            <Button onClick={onClose}>关闭</Button>
            <Button type="primary" icon={<PrinterOutlined />} disabled={busy} onClick={doPrint}>
              打印 {list.length} 张
            </Button>
          </Space>
        }
      >
        <Space direction="vertical" size={14} style={{ width: "100%" }}>
          <Alert
            type="warning"
            showIcon
            message="打印前先确认下面的地址"
            description="这是巡检员扫码后会打开的地址。填错了，贴纸得全部撕下来重贴——而这件事贴之前看不出来。"
          />
          <Space wrap>
            <span style={{ color: "#666" }}>移动端地址</span>
            <Input
              value={origin}
              onChange={(e) => setOrigin(e.target.value)}
              style={{ width: 340 }}
              placeholder="https://your-domain.com"
            />
          </Space>
          <Space wrap size={16}>
            <Space>
              <span style={{ color: "#666" }}>贴纸尺寸</span>
              <Segmented
                value={size}
                onChange={(v) => setSize(v as SizeKey)}
                options={(Object.keys(SIZES) as SizeKey[]).map((k) => ({
                  value: k,
                  label: `${k}（${SIZES[k].qr}mm）`,
                }))}
              />
            </Space>
            <Space>
              <span style={{ color: "#666" }}>抗污损</span>
              <Segmented
                value={ecc}
                onChange={(v) => setEcc(v as EccKey)}
                options={(Object.keys(ECC) as EccKey[]).map((k) => ({ value: k, label: k }))}
              />
            </Space>
          </Space>

          {/* 【把可扫性直接算给人看】否则管理员只能靠"看着挺清楚"来判断，
              而看着清楚和手机扫得出来是两回事，等贴完才发现就晚了。 */}
          {moduleMM && (
            <Alert
              type={tooDense ? "error" : "success"}
              showIcon
              message={
                tooDense
                  ? `每个小格只有 ${moduleMM.mm.toFixed(2)}mm —— 手机可能扫不出来`
                  : `每个小格 ${moduleMM.mm.toFixed(2)}mm，一般手机可稳定扫出`
              }
              description={
                tooDense
                  ? "机房光线差、手机要贴很近才对焦，小于 0.5mm 很容易失败。请换大一号贴纸，或把抗污损调低一档。"
                  : `按最长的设备编号计算，${moduleMM.modules}×${moduleMM.modules} 格。`
              }
            />
          )}

          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            共 {list.length} 台。打印范围跟随台账当前的筛选条件——想只印某个项目，先在台账里筛好再进来。
          </Typography.Text>

          {busy ? (
            <div style={{ padding: 40, textAlign: "center" }}>
              <Spin tip="生成中…" />
            </div>
          ) : (
            <div
              style={{
                maxHeight: 320,
                overflowY: "auto",
                background: "#fafafa",
                border: "1px solid #f0f0f0",
                padding: 12,
                display: "grid",
                gridTemplateColumns: `repeat(auto-fill, minmax(${spec.w * 3}px, 1fr))`,
                gap: 10,
              }}
            >
              {list.map((a) => (
                <div key={a.id} style={{ background: "#fff", border: "1px solid #eee", padding: 8, textAlign: "center" }}>
                  {codes[a.id] ? (
                    <img src={codes[a.id]} alt="" style={{ width: "100%", display: "block" }} />
                  ) : (
                    <div style={{ padding: 16, color: "#d4380d", fontSize: 12 }}>生成失败</div>
                  )}
                  <div style={{ fontSize: 12, fontWeight: 600, marginTop: 4 }}>{a.assetName || a.assetKey}</div>
                  <div style={{ fontSize: 11, color: "#888" }}>{a.project}</div>
                </div>
              ))}
            </div>
          )}
        </Space>
      </Modal>

      {/*
        打印用的那一份【不在弹窗里】。
        弹窗是浮层，打印时的定位和分页都不受控；而且它有滚动容器，
        打印只会出现可视区域的那几张。所以单独渲染一份常态隐藏的贴纸页，
        只在 @media print 下显形。
      */}
      {open && (
        <div className="qr-print-root">
          {list.map((a) => (
            <div className="qr-label" key={a.id}>
              {codes[a.id] && <img src={codes[a.id]} alt="" />}
              <div className="qr-name">{a.assetName || a.assetKey}</div>
              <div className="qr-code">{a.assetKey || ""}</div>
              <div className="qr-proj">{a.project}</div>
            </div>
          ))}
        </div>
      )}

      <style>{`
        .qr-print-root { display: none; }
        @media print {
          /* 用 visibility 而不是 display:none 藏其余内容 —— display 会让布局塌掉,
             打印时容易出现整页空白或错位的分页 */
          body * { visibility: hidden !important; }
          .qr-print-root, .qr-print-root * { visibility: visible !important; }
          .qr-print-root {
            display: flex !important;
            flex-wrap: wrap;
            align-content: flex-start;
            position: absolute; left: 0; top: 0; width: 100%;
            gap: 4mm;
            padding: 6mm;
            background: #fff;
          }
          .qr-label {
            width: ${spec.w}mm;
            text-align: center;
            /* 一张贴纸不能被分页切成两半 —— 切开的码永远扫不出来 */
            break-inside: avoid;
            page-break-inside: avoid;
            border: 0.3mm solid #ddd;
            border-radius: 1mm;
            padding: 2mm 1mm 2.5mm;
            color: #000;
          }
          .qr-label img { width: ${spec.qr}mm; height: ${spec.qr}mm; display: block; margin: 0 auto; }
          .qr-name { font-size: ${spec.name}pt; font-weight: 700; margin-top: 1.5mm; line-height: 1.2; }
          .qr-code { font-size: ${spec.code}pt; margin-top: 0.6mm; }
          .qr-proj { font-size: ${spec.code}pt; color: #555; margin-top: 0.3mm; }
          @page { margin: 8mm; }
        }
      `}</style>
    </>
  );
}
