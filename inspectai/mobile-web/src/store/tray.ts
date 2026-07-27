import { create } from "zustand";

import { compressImage, getGeo } from "@/lib/capture";
import { addShot, listShots, PendingShot, removeShot } from "@/lib/offlineStore";

interface TrayState {
  shots: PendingShot[];
  /** 浏览器报告的联网状态。离线时托盘只存不传 */
  online: boolean;
  busy: boolean;
  /** 从 IndexedDB 载入托盘(启动时、上传后调用) */
  refresh: () => Promise<void>;
  /** 拍照/选图后入托盘:压缩 → 取定位 → 落 IndexedDB */
  addFiles: (files: File[], userId: string) => Promise<void>;
  remove: (id: string) => Promise<void>;
}

export const useTray = create<TrayState>((set, get) => ({
  shots: [],
  online: navigator.onLine,
  busy: false,

  async refresh() {
    set({ shots: await listShots() });
  },

  async addFiles(files, userId) {
    if (!files.length) return;
    set({ busy: true });
    try {
      // 定位取一次给这批照片共用:同一次拍摄位置相同,且避免连续多次定位拖慢
      const geo = await getGeo();
      for (const file of files) {
        const compressed = await compressImage(file);
        await addShot({
          blob: compressed,
          fileName: compressed.name,
          geo,
          userId,
          capturedAt: new Date().toISOString(),
        });
      }
      await get().refresh();
    } finally {
      set({ busy: false });
    }
  },

  async remove(id) {
    await removeShot(id);
    await get().refresh();
  },
}));

// 联网状态变化时同步到 store,供 UI 显示"离线暂存中"
window.addEventListener("online", () => useTray.setState({ online: true }));
window.addEventListener("offline", () => useTray.setState({ online: false }));
