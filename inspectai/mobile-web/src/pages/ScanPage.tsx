import { Button, Toast } from "@/ui";
import jsQR from "jsqr";
import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import FlowHeader from "@/components/FlowHeader";
import { AssetQRTarget, SCAN_PARAM, parseScannedText, splitAssetID } from "@/lib/assetQR";
import { AssetDTO, getAsset } from "@/api/inspection";
import { setRetakeTarget } from "@/store/retake";

// ===== 扫码认设备 =====
//
// 一台设备一个码,贴在设备上。扫一下直接进这台设备的巡检。
//
// 【解决的是"认错设备"】在这之前,设备编号靠 AI 从照片里认。KT-7 / K7 / KT-70
// 认错一个字符,记录就落到别的设备上,而台账要过很久才发现对不上账。
// 扫码把这一步从"猜"变成"读"。
//
// 两种进来的方式,都要支持:
//   1. app 内点「扫码」→ 开摄像头 → 扫 → 认出设备
//   2. 微信/系统相机扫了贴纸 → 直接带着 ?a= 打开这一页 → 【不需要摄像头】
// 第 2 种是现场最常发生的,因为大家习惯性掏出微信扫一扫。

type Phase = "idle" | "scanning" | "found" | "error";

export default function ScanPage() {
  const nav = useNavigate();
  const [params] = useSearchParams();
  const videoRef = useRef<HTMLVideoElement | null>(null);
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const streamRef = useRef<MediaStream | null>(null);
  const rafRef = useRef<number>(0);

  const [phase, setPhase] = useState<Phase>("idle");
  const [errText, setErrText] = useState("");
  const [target, setTarget] = useState<AssetQRTarget | null>(null);
  const [asset, setAsset] = useState<AssetDTO | null>(null);
  const [loadingAsset, setLoadingAsset] = useState(false);

  // 彻底关掉摄像头。【必须做】不关的话手机上的摄像头指示灯一直亮着,
  // 用户会以为 app 在偷拍;而且很费电,巡检一上午手机就没了。
  const stopCamera = useCallback(() => {
    if (rafRef.current) cancelAnimationFrame(rafRef.current);
    rafRef.current = 0;
    streamRef.current?.getTracks().forEach((t) => t.stop());
    streamRef.current = null;
  }, []);

  // 认出一台设备之后:停摄像头,联网时把详情取回来做二次确认
  const onHit = useCallback(
    (hit: AssetQRTarget) => {
      stopCamera();
      setTarget(hit);
      setPhase("found");
      setLoadingAsset(true);
      getAsset(hit.assetId)
        .then((a) => setAsset(a))
        .catch(() => {
          // 【取不到详情不算失败】可能是没信号,也可能这台设备不在他的项目范围内。
          // 编号和项目从码里就能拿到,足够开始巡检 —— 机房没信号是常态,
          // 不能因为查不到名字就让人干不了活。
          setAsset(null);
        })
        .finally(() => setLoadingAsset(false));
    },
    [stopCamera],
  );

  // 入口 1:URL 里直接带了资产 ID(微信/系统相机扫的)
  useEffect(() => {
    const raw = params.get(SCAN_PARAM);
    if (!raw) return;
    const hit = splitAssetID(raw);
    if (hit) onHit(hit);
    else {
      setPhase("error");
      setErrText("这个码不是本系统的设备码");
    }
  }, [params, onHit]);

  // 入口 2:开摄像头扫
  const startCamera = useCallback(async () => {
    setErrText("");
    // 摄像头只在安全上下文里可用。用 http 打开(比如直接敲局域网 IP)会直接没有
    // mediaDevices —— 这时候要说清楚原因,否则用户只看到一片黑,以为 app 坏了。
    if (!window.isSecureContext || !navigator.mediaDevices?.getUserMedia) {
      setPhase("error");
      setErrText(
        "当前地址不支持调用摄像头(需要 https)。请用微信或手机相机直接扫设备上的二维码，一样能进入。",
      );
      return;
    }
    try {
      const stream = await navigator.mediaDevices.getUserMedia({
        // 后置摄像头。不写的话手机会开前置,对着自己的脸扫
        video: { facingMode: { ideal: "environment" } },
        audio: false,
      });
      streamRef.current = stream;
      const video = videoRef.current;
      if (!video) return;
      video.srcObject = stream;
      video.setAttribute("playsinline", "true"); // iOS 上不加会强制全屏播放
      await video.play();
      setPhase("scanning");
      tick();
    } catch (e) {
      setPhase("error");
      const name = (e as { name?: string })?.name || "";
      setErrText(
        name === "NotAllowedError"
          ? "没有拿到摄像头权限。请在浏览器设置里允许使用摄像头，或直接用微信扫设备上的码。"
          : "打不开摄像头。可以改用微信或手机相机扫设备上的二维码。",
      );
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 逐帧取图解码。
  //
  // 【只取中间那一块去解码】整帧解码在旧手机上会掉到几帧每秒,
  // 而人本来就会把码对准取景框中央 —— 少解一大圈背景,快很多也更准。
  function tick() {
    const video = videoRef.current;
    const canvas = canvasRef.current;
    if (!video || !canvas || video.readyState !== video.HAVE_ENOUGH_DATA) {
      rafRef.current = requestAnimationFrame(tick);
      return;
    }
    const side = Math.min(video.videoWidth, video.videoHeight);
    const box = Math.floor(side * 0.7);
    canvas.width = box;
    canvas.height = box;
    const ctx = canvas.getContext("2d", { willReadFrequently: true });
    if (!ctx) return;
    ctx.drawImage(
      video,
      (video.videoWidth - box) / 2,
      (video.videoHeight - box) / 2,
      box,
      box,
      0,
      0,
      box,
      box,
    );
    const img = ctx.getImageData(0, 0, box, box);
    const code = jsQR(img.data, img.width, img.height, {
      inversionAttempts: "dontInvert", // 贴纸都是黑码白底,不用试反色,省一半时间
    });
    if (code?.data) {
      const hit = parseScannedText(code.data);
      if (hit) {
        onHit(hit);
        return;
      }
      // 扫到了码但不是本系统的。【提示一下继续扫】不能静默 ——
      // 用户会一直举着手机等,不知道是没扫到还是扫错了。
      Toast.show({ content: "这不是设备码，换一个试试" });
    }
    rafRef.current = requestAnimationFrame(tick);
  }

  // 【进来就开摄像头】扫码页只有一件事可做,还要先点一下"打开摄像头"
  // 等于凭空多一步 —— 巡检员举着手机站在设备前,每一下多余的点击都很烦。
  //
  // 被二维码带着 ?a= 进来的不开:那条路已经知道是哪台设备了,
  // 开摄像头纯属白费电,还会弹一次没必要的权限询问。
  useEffect(() => {
    if (params.get(SCAN_PARAM)) return;
    if (phase !== "idle") return;
    void startCamera();
    // 只在首次进入时自动开;用户手动点了"停止"之后不该又被自动打开
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => stopCamera, [stopCamera]);

  function startInspect() {
    if (!target) return;
    // 复用「复检」那套直达机制:带上模板跳过 AI 分类、带上编号让后端认归属。
    // 扫码本质上就是它的另一个入口,没必要另造一条链路。
    setRetakeTarget({
      templateId: asset?.templateId || target.templateId,
      pointId: asset?.pointId || "",
      assetNo: asset?.assetKey || target.assetNo,
      assetName: asset?.assetName || target.assetNo,
    });
    Toast.show({ content: `对准「${asset?.assetName || target.assetNo}」拍照即可` });
    nav("/");
  }

  return (
    <div className="flow-screen scan-screen">
      <FlowHeader title="扫码识别设备" onBack={() => nav(-1)} />

      {phase === "found" && target ? (
        <div className="scan-body">
          <div className="scan-card">
            <div className="scan-card-no">{asset?.assetName || target.assetNo}</div>
            <div className="scan-card-rows">
              <div>
                <span>编号</span>
                <b>{asset?.assetKey || target.assetNo}</b>
              </div>
              <div>
                <span>项目</span>
                <b>{asset?.project || target.project}</b>
              </div>
              {asset?.lastStatus && (
                <div>
                  <span>当前状态</span>
                  <b>{asset.lastStatus}</b>
                </div>
              )}
            </div>
            {!loadingAsset && !asset && (
              // 说清楚是"没查到"而不是"扫错了" —— 这两种情况用户的下一步完全不同
              <div className="scan-card-tip">
                没能取到这台设备的档案（可能没信号，或它不在你负责的项目里）。
                仍可按码上的编号继续巡检。
              </div>
            )}
          </div>
          <div className="scan-actions">
            <Button className="btn-primary" block type="primary" onClick={startInspect}>
              开始巡检
            </Button>
            <Button
              className="btn-ghost"
              block
              type="ghost"
              onClick={() => nav(`/asset/${encodeURIComponent(target.assetId)}`)}
            >
              查看设备详情
            </Button>
            <Button
              className="btn-ghost"
              block
              type="ghost"
              onClick={() => {
                setTarget(null);
                setAsset(null);
                setPhase("idle");
              }}
            >
              重新扫
            </Button>
          </div>
        </div>
      ) : (
        <div className="scan-body">
          <div className="scan-view">
            <video ref={videoRef} className="scan-video" muted playsInline />
            <canvas ref={canvasRef} style={{ display: "none" }} />
            {phase !== "scanning" && (
              <div className="scan-placeholder">
                {phase === "error" ? errText : "对准设备上的二维码"}
              </div>
            )}
            {phase === "scanning" && <div className="scan-frame" />}
          </div>
          <div className="scan-actions">
            {phase === "scanning" ? (
              <Button className="btn-ghost" block type="ghost" onClick={() => { stopCamera(); setPhase("idle"); }}>
                停止
              </Button>
            ) : (
              <Button className="btn-primary" block type="primary" onClick={() => void startCamera()}>
                打开摄像头
              </Button>
            )}
            <div className="scan-hint">
              也可以直接用微信「扫一扫」扫设备上的码，同样会打开这一页。
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
