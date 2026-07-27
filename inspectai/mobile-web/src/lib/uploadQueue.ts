// ===== 上传队列 =====
//
// 联网后把离线仓库里的照片逐张送上去。设计要点:
//   1. 串行上传 —— 弱网下并发只会让每一路都超时,一次一张反而更快传完
//   2. 错误分类 —— 网络问题可重试;服务端明确拒绝(4xx)不该无限重试
//   3. 指数退避 —— 失败后等待时间递增,避免没信号时疯狂重试耗电
//   4. 幂等键 —— 弱网下"其实传成功了但响应没回来"的情况,重放不会产生重复记录

import { getToken } from "@/api/client";
import { PendingShot, listShots, removeShot, updateShot } from "@/lib/offlineStore";

/** 退避梯度(秒):5s → 15s → 1m → 5m → 15m,之后维持 15m */
const BACKOFF_SECONDS = [5, 15, 60, 300, 900];
/** 超过这个次数仍是网络失败,就不再自动重试,标记为需人工处理 */
const MAX_AUTO_RETRIES = 8;

function backoffMs(retries: number): number {
  const idx = Math.min(retries, BACKOFF_SECONDS.length - 1);
  return BACKOFF_SECONDS[idx] * 1000;
}

/** 上传单张。返回值只表达"这一张后续怎么处理" */
type UploadOutcome = { kind: "done" } | { kind: "retry"; reason: string } | { kind: "blocked"; reason: string };

async function uploadOne(shot: PendingShot): Promise<UploadOutcome> {
  const fd = new FormData();
  fd.append("files", new File([shot.blob], shot.fileName, { type: shot.blob.type || "image/jpeg" }));
  fd.append("capturedAt", shot.capturedAt);
  if (shot.geo) {
    fd.append("lat", String(shot.geo.lat));
    fd.append("lng", String(shot.geo.lng));
    fd.append("accuracy", String(shot.geo.accuracy));
  }

  let res: Response;
  try {
    res = await fetch("/api/inspection/offline-shots", {
      method: "POST",
      headers: {
        "X-InspectAI-Token": getToken(),
        // 幂等键:后端据此识别重放,弱网重复提交不产生重复记录
        "Idempotency-Key": shot.idempotencyKey,
      },
      body: fd,
    });
  } catch (err) {
    // fetch 抛异常 = 网络层问题(断网/超时/DNS),一定可重试
    return { kind: "retry", reason: err instanceof Error ? err.message : "网络异常" };
  }

  if (res.ok) {
    // 不能只看 res.ok 就判定成功 —— 后端对未知 /api 路径会返回 200 + 前端 HTML,
    // 误判会导致"照片没传上去却被本地删掉"的静默丢失。必须拿到结构正确的 JSON
    // 确认才算成功;拿不到就当服务端异常,留在本地继续重试。
    const ctype = res.headers.get("content-type") || "";
    if (!ctype.includes("application/json")) {
      return { kind: "retry", reason: "服务端返回异常(非 JSON),照片已保留" };
    }
    try {
      const body = await res.json();
      if (!body || typeof body !== "object" || !("imageId" in body || "id" in body || "ok" in body)) {
        return { kind: "retry", reason: "服务端响应缺少上传结果,照片已保留" };
      }
    } catch {
      return { kind: "retry", reason: "服务端响应无法解析,照片已保留" };
    }
    return { kind: "done" };
  }

  // 401/403:登录态问题 —— 重试无意义,等用户重新登录
  if (res.status === 401 || res.status === 403) {
    return { kind: "blocked", reason: "登录已失效,请重新登录后重试" };
  }
  // 其余 4xx:服务端明确拒绝(文件格式/大小/参数),重试同样会被拒
  if (res.status >= 400 && res.status < 500) {
    let msg = `服务器拒绝(${res.status})`;
    try {
      const body = await res.json();
      if (body?.message) msg = body.message;
    } catch {
      /* 响应体不是 JSON 就用默认文案 */
    }
    return { kind: "blocked", reason: msg };
  }
  // 5xx / 其他:服务端临时故障,可重试
  return { kind: "retry", reason: `服务器暂时不可用(${res.status})` };
}

let running = false;

/**
 * 跑一轮队列:把到期的待上传项串行传完。
 * 并发调用会被忽略(running 锁),避免 online 事件与定时器同时触发跑两遍。
 *
 * @param onProgress 每张处理完回调一次,供 UI 刷新
 * @returns 本轮成功上传的张数
 */
export async function runQueue(onProgress?: () => void): Promise<number> {
  if (running || !navigator.onLine) return 0;
  running = true;
  let uploaded = 0;
  try {
    const now = Date.now();
    const all = await listShots();
    const due = all.filter(
      (s) => (s.status === "pending" || s.status === "failed") && s.nextRetryAt <= now,
    );

    for (const shot of due) {
      // 每张之前重新确认还在线,断网了立刻停,不做无谓尝试
      if (!navigator.onLine) break;

      await updateShot(shot.id, { status: "uploading" });
      onProgress?.();

      const outcome = await uploadOne(shot);
      if (outcome.kind === "done") {
        await removeShot(shot.id); // 传成功即从本地清除,不占空间
        uploaded += 1;
      } else if (outcome.kind === "blocked") {
        await updateShot(shot.id, { status: "blocked", lastError: outcome.reason });
      } else {
        const retries = shot.retries + 1;
        if (retries >= MAX_AUTO_RETRIES) {
          await updateShot(shot.id, {
            status: "blocked",
            retries,
            lastError: `多次上传失败:${outcome.reason}`,
          });
        } else {
          await updateShot(shot.id, {
            status: "failed",
            retries,
            nextRetryAt: Date.now() + backoffMs(retries),
            lastError: outcome.reason,
          });
        }
      }
      onProgress?.();
    }
  } finally {
    running = false;
  }
  return uploaded;
}

/** 用户手动点"立即重试":清掉退避等待,让下一轮马上处理 */
export async function retryNow(id: string): Promise<void> {
  await updateShot(id, { status: "pending", nextRetryAt: 0, lastError: undefined });
}
