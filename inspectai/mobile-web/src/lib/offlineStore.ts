// ===== 离线仓库(IndexedDB) =====
//
// 弱网现场的地基:拍下的照片先落本地,联网后再上传补 AI 识别。
//
// 为什么是 IndexedDB 而不是 localStorage:
//   - localStorage 上限约 5MB,且只能存字符串(照片转 base64 还要涨 33%)
//   - 一次巡检几张原图就是几 MB,只有 IndexedDB 能直接存 Blob 且容量足够
//   - IndexedDB 持久化到磁盘,关屏/切后台/重启浏览器数据都在
//
// 证据链设计:capturedAt 由手机盖(声称的拍摄时间,仅供参考),
// 服务器收到时间才是权威。两者都保留并在记录上分开展示,不隐藏离线时间差。

const DB_NAME = "inspectai-offline";
const DB_VERSION = 1;
const STORE_SHOTS = "shots";

/** 一张待上传的照片(托盘里的一项) */
export interface PendingShot {
  /** 本地 ID,同时用作 IndexedDB 主键 */
  id: string;
  /** 幂等键:来网重放时带给后端,弱网重复提交不会产生重复记录 */
  idempotencyKey: string;
  /** 原图。不做任何叠加,保留为证据 */
  blob: Blob;
  fileName: string;
  /** 拍摄时间(手机盖,ISO 字符串) */
  capturedAt: string;
  /** 拍照时的定位,拿不到就是 null */
  geo: { lat: number; lng: number; accuracy: number } | null;
  /** 归属巡检员,便于多账号共用一台设备时区分 */
  userId: string;
  /** pending 待上传 / uploading 上传中 / failed 失败待重试 */
  status: "pending" | "uploading" | "failed";
  /** 失败次数,用于退避与提示 */
  retries: number;
  /** 最近一次失败原因,展示给用户 */
  lastError?: string;
}

let dbPromise: Promise<IDBDatabase> | null = null;

function openDB(): Promise<IDBDatabase> {
  if (dbPromise) return dbPromise;
  dbPromise = new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE_SHOTS)) {
        const store = db.createObjectStore(STORE_SHOTS, { keyPath: "id" });
        // 按拍摄时间排序展示;按状态挑出待上传项
        store.createIndex("capturedAt", "capturedAt");
        store.createIndex("status", "status");
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
  return dbPromise;
}

function tx<T>(mode: IDBTransactionMode, fn: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  return openDB().then(
    (db) =>
      new Promise<T>((resolve, reject) => {
        const t = db.transaction(STORE_SHOTS, mode);
        const req = fn(t.objectStore(STORE_SHOTS));
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error);
      }),
  );
}

function newId(prefix: string): string {
  return `${prefix}_${Date.now().toString(36)}_${Math.random().toString(36).slice(2, 10)}`;
}

/** 存一张照片进托盘 */
export async function addShot(input: {
  blob: Blob;
  fileName: string;
  capturedAt?: string;
  geo?: PendingShot["geo"];
  userId: string;
}): Promise<PendingShot> {
  const shot: PendingShot = {
    id: newId("shot"),
    idempotencyKey: newId("idem"),
    blob: input.blob,
    fileName: input.fileName,
    capturedAt: input.capturedAt || new Date().toISOString(),
    geo: input.geo ?? null,
    userId: input.userId,
    status: "pending",
    retries: 0,
  };
  await tx("readwrite", (s) => s.add(shot));
  return shot;
}

/** 取全部待上传项,按拍摄时间正序(先拍的先传) */
export async function listShots(): Promise<PendingShot[]> {
  const all = await tx<PendingShot[]>("readonly", (s) => s.getAll() as IDBRequest<PendingShot[]>);
  return all.sort((a, b) => a.capturedAt.localeCompare(b.capturedAt));
}

export async function countShots(): Promise<number> {
  return tx<number>("readonly", (s) => s.count());
}

/** 局部更新一项(改状态 / 记失败原因) */
export async function updateShot(id: string, patch: Partial<PendingShot>): Promise<void> {
  const cur = await tx<PendingShot | undefined>(
    "readonly",
    (s) => s.get(id) as IDBRequest<PendingShot | undefined>,
  );
  if (!cur) return;
  await tx("readwrite", (s) => s.put({ ...cur, ...patch }));
}

/** 上传成功后清除;用户手动删除也走这里 */
export async function removeShot(id: string): Promise<void> {
  await tx("readwrite", (s) => s.delete(id));
}

/** 清空托盘(仅调试/退出登录时用) */
export async function clearShots(): Promise<void> {
  await tx("readwrite", (s) => s.clear());
}
