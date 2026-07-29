import { create } from "zustand";

import { compressImage, getGeo } from "@/lib/capture";
import {
  PendingShot,
  StorageFullError,
  addShot,
  getStorageInfo,
  listShots,
  recoverStaleUploads,
  removeShot,
  removeShots,
  requestPersistentStorage,
} from "@/lib/offlineStore";
import { retryNow, runQueue, unblockAll } from "@/lib/uploadQueue";
import { getStoredUser } from "@/api/client";

/** 当前登录用户 id。直接读存储而非引 auth store,避免循环依赖 */
function currentUserId(): string | undefined {
  return getStoredUser()?.id;
}

/** 队列自动跑一轮的间隔:覆盖"信号悄悄恢复但没触发 online 事件"的情况 */
const TICK_MS = 30_000;

interface PendingState {
  shots: PendingShot[];
  online: boolean;
  /** 正在处理拍照入库(压缩/定位),期间快门置灰 */
  saving: boolean;
  /** 存储是否已获持久化保证。false 时提示用户数据可能被系统清理 */
  persisted: boolean;
  /** 已用空间字节,用于"空间快满了"提示 */
  usedBytes: number;
  freeBytes: number | null;
  /**
   * 上传成功计数。每传成功一批就自增 —— 服务器上的"待处理"数量随之变化,
   * 首页红点据此重新拉取。不这么做的话红点要等切走再切回来才更新,
   * 用户拍完照会以为没生效。
   */
  uploadedTick: number;

  init: () => Promise<void>;
  refresh: () => Promise<void>;
  addFiles: (files: File[], userId: string) => Promise<{ added: number; error?: string }>;
  remove: (id: string) => Promise<void>;
  /** 批量删除选中的照片 */
  removeMany: (ids: string[]) => Promise<number>;
  retry: (id: string) => Promise<void>;
  /** 手动触发上传(用户点"立即上传") */
  flush: () => Promise<number>;
  /** 解除被拦下的照片并重排队;onlyAuth=只解除"登录失效"那批 */
  unblock: (onlyAuth?: boolean) => Promise<number>;
}

export const usePending = create<PendingState>((set, get) => ({
  shots: [],
  online: navigator.onLine,
  saving: false,
  persisted: false,
  usedBytes: 0,
  freeBytes: null,
  uploadedTick: 0,

  async init() {
    // 登录态可能刚刷新过:把因"登录失效"被拦下的照片放回队列 ——
    // 阻塞原因已经消失,不该还要用户逐张点重试。
    await unblockAll(true);
    // 1) 申请持久化:不申请的话浏览器在空间紧张时可直接清掉我们的照片
    const persisted = await requestPersistentStorage();
    // 2) 复位上次残留的 uploading —— 否则页面被中途关掉的那几张会永远卡住
    await recoverStaleUploads();
    set({ persisted });
    await get().refresh();
    void get().flush();
  },

  async refresh() {
    // 按当前用户过滤:共用手机时不显示别人的照片
    const [shots, info] = await Promise.all([listShots(currentUserId()), getStorageInfo()]);
    set({ shots, usedBytes: info.usedBytes, freeBytes: info.freeBytes, persisted: info.persisted });
  },

  async addFiles(files, userId) {
    if (!files.length) return { added: 0 };
    set({ saving: true });
    try {
      // 定位取一次给这批共用:同次拍摄位置相同,也避免连续定位拖慢
      const geo = await getGeo();
      let added = 0;
      for (const file of files) {
        const compressed = await compressImage(file);
        try {
          await addShot({
            blob: compressed,
            fileName: compressed.name,
            geo,
            userId,
            capturedAt: new Date().toISOString(),
          });
          added += 1;
        } catch (err) {
          await get().refresh();
          if (err instanceof StorageFullError) return { added, error: err.message };
          return { added, error: "保存失败,请重试" };
        }
      }
      await get().refresh();
      void get().flush(); // 在线就立刻开始传,不用等定时器
      return { added };
    } finally {
      set({ saving: false });
    }
  },

  async remove(id) {
    await removeShot(id);
    await get().refresh();
  },

  async removeMany(ids) {
    const n = await removeShots(ids);
    await get().refresh();
    return n;
  },

  async retry(id) {
    await retryNow(id);
    await get().refresh();
    void get().flush();
  },

  async unblock(onlyAuth = false) {
    const n = await unblockAll(onlyAuth);
    if (n > 0) {
      await get().refresh();
      void get().flush();
    }
    return n;
  },

  async flush() {
    const n = await runQueue(() => void get().refresh());
    await get().refresh();
    // 传成功了 → 服务器待处理数变了 → 通知首页红点刷新
    if (n > 0) set({ uploadedTick: get().uploadedTick + 1 });
    return n;
  },
}));

// 联网即冲一轮;定时兜底覆盖"信号悄悄恢复"的情况
window.addEventListener("online", () => {
  usePending.setState({ online: true });
  void usePending.getState().flush();
});
window.addEventListener("offline", () => usePending.setState({ online: false }));
setInterval(() => {
  if (navigator.onLine) void usePending.getState().flush();
}, TICK_MS);
