// ===== 离线仓库(IndexedDB) =====
//
// 弱网现场的地基:拍下的照片先落本地,联网后自动上传补 AI 识别。
//
// 为什么是 IndexedDB 而不是 localStorage:
//   - localStorage 上限约 5MB,且只能存字符串(照片转 base64 还要涨 33%)
//   - 一次巡检几张原图就是几 MB,只有 IndexedDB 能直接存 Blob 且容量足够
//   - IndexedDB 持久化到磁盘,关屏/切后台/重启浏览器数据都在
//
// 证据链:capturedAt 由手机盖(声称的拍摄时间,仅供参考),服务器收到时间才是
// 权威。两者都保留、在记录上分开展示,不隐藏离线时间差。

const DB_NAME = "inspectai-offline";
const DB_VERSION = 1;
const STORE_SHOTS = "shots";

/** 一条待上传照片的生命周期状态 */
export type ShotStatus =
  | "pending" // 待上传
  | "uploading" // 上传中
  | "failed" // 可重试的失败(网络问题),会自动退避重试
  | "blocked"; // 不可自动重试(服务端拒绝 / 文件有问题),需人工处理

export interface PendingShot {
  /** 本地 ID,同时用作 IndexedDB 主键 */
  id: string;
  /** 幂等键:来网重放时带给后端,弱网重复提交不会产生重复记录 */
  idempotencyKey: string;
  /** 原图。不做任何叠加,保留为证据 */
  blob: Blob;
  fileName: string;
  size: number;
  /** 拍摄时间(手机盖,ISO 字符串) */
  capturedAt: string;
  /** 拍照时的定位,拿不到就是 null */
  geo: { lat: number; lng: number; accuracy: number } | null;
  /** 归属巡检员,便于多账号共用一台设备时区分 */
  userId: string;
  /**
   * 这张拍的是哪台设备(扫码时锁定的那台)。空 = 手动路径拍的,没指定。
   *
   * 【必须在拍的时候记】原来照片是"无主"的,归属由成单那一刻的扫码上下文决定,
   * 于是一次巡多台会串:扫 A 拍几张、走到 B 扫 B 再拍几张,进"选照片"时
   * 全混在一起,而上下文是 B —— 全选就全落到 B 上,A 等于没巡。
   */
  assetId?: string;
  status: ShotStatus;
  /** 失败次数,用于指数退避 */
  retries: number;
  /** 下次可重试的时间戳(ms)。退避期内跳过,避免弱网下疯狂重试耗电 */
  nextRetryAt: number;
  /** 最近一次失败原因,展示给用户 */
  lastError?: string;
  /**
   * blocked 的原因分类:
   *   auth     登录失效 —— 重新登录后即可自动恢复,不该让用户逐张点重试
   *   rejected 服务端明确拒绝(格式/大小/参数)—— 重登也没用,需人工处理
   */
  blockedKind?: "auth" | "rejected";
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
        store.createIndex("capturedAt", "capturedAt");
        store.createIndex("status", "status");
      }
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
  return dbPromise;
}

function tx<T>(
  mode: IDBTransactionMode,
  fn: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
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

// ===== 存储可靠性 =====

/**
 * 申请「持久化存储」。不申请的话,浏览器在设备空间紧张时可以直接清掉我们的
 * IndexedDB —— 对一个"照片绝不能丢"的功能来说这是致命的。
 * 返回是否已获得持久化保证(部分浏览器会静默拒绝,那就只能尽力而为)。
 */
export async function requestPersistentStorage(): Promise<boolean> {
  try {
    if (!navigator.storage?.persist) return false;
    if (await navigator.storage.persisted()) return true;
    return await navigator.storage.persist();
  } catch {
    return false;
  }
}

export interface StorageInfo {
  usedBytes: number;
  quotaBytes: number;
  /** 剩余可用字节。拿不到配额信息时为 null */
  freeBytes: number | null;
  persisted: boolean;
}

/** 查存储用量,给"空间快满了"的提前告警用 */
export async function getStorageInfo(): Promise<StorageInfo> {
  let usedBytes = 0;
  let quotaBytes = 0;
  let persisted = false;
  try {
    const est = await navigator.storage?.estimate?.();
    usedBytes = est?.usage ?? 0;
    quotaBytes = est?.quota ?? 0;
    persisted = (await navigator.storage?.persisted?.()) ?? false;
  } catch {
    /* 拿不到就按未知处理 */
  }
  return {
    usedBytes,
    quotaBytes,
    freeBytes: quotaBytes > 0 ? Math.max(0, quotaBytes - usedBytes) : null,
    persisted,
  };
}

/** 空间不足以再存这么大一张时抛出,调用方据此提示用户先联网上传 */
export class StorageFullError extends Error {
  constructor() {
    super("手机存储空间不足,请先联网上传已拍照片");
    this.name = "StorageFullError";
  }
}

// ===== 增删改查 =====

/** 存一张照片。空间不足会抛 StorageFullError,不会静默失败 */
export async function addShot(input: {
  blob: Blob;
  fileName: string;
  capturedAt?: string;
  geo?: PendingShot["geo"];
  userId: string;
  /** 扫码锁定的设备。手动路径不传 */
  assetId?: string;
}): Promise<PendingShot> {
  // 预留 20% 余量:配额用满会直接写失败,提前拦住给用户可行动的提示
  const info = await getStorageInfo();
  if (info.freeBytes !== null && input.blob.size * 1.2 > info.freeBytes) {
    throw new StorageFullError();
  }

  const shot: PendingShot = {
    id: newId("shot"),
    idempotencyKey: newId("idem"),
    blob: input.blob,
    fileName: input.fileName,
    size: input.blob.size,
    capturedAt: input.capturedAt || new Date().toISOString(),
    geo: input.geo ?? null,
    userId: input.userId,
    assetId: input.assetId,
    status: "pending",
    retries: 0,
    nextRetryAt: 0,
  };
  try {
    await tx("readwrite", (s) => s.add(shot));
  } catch (err) {
    // QuotaExceededError 兜底:预检算漏了也不能让用户以为存上了
    if (err instanceof DOMException && err.name === "QuotaExceededError") {
      throw new StorageFullError();
    }
    throw err;
  }
  return shot;
}

/**
 * 待上传项,按拍摄时间正序(先拍的先传)。
 *
 * @param userId 传入则只返回该用户的照片。**必须过滤** —— 巡检现场常多人共用
 *   一台手机:A 拍完没信号就退出、B 登录后若看得到 A 的照片并传上去,
 *   记录会算到 B 头上,归属就错了。照片留在本地不删,A 回来登录仍能看到。
 *   不传 = 全部(仅供迁移/清理这类维护逻辑使用)。
 */
export async function listShots(userId?: string): Promise<PendingShot[]> {
  const all = await tx<PendingShot[]>(
    "readonly",
    (s) => s.getAll() as IDBRequest<PendingShot[]>,
  );
  const scoped = userId
    ? all.filter((s) => !s.userId || s.userId === userId)
    : all;
  return scoped.sort((a, b) => a.capturedAt.localeCompare(b.capturedAt));
}

export async function getShot(id: string): Promise<PendingShot | undefined> {
  return tx<PendingShot | undefined>(
    "readonly",
    (s) => s.get(id) as IDBRequest<PendingShot | undefined>,
  );
}

export async function updateShot(
  id: string,
  patch: Partial<PendingShot>,
): Promise<void> {
  const cur = await getShot(id);
  if (!cur) return;
  await tx("readwrite", (s) => s.put({ ...cur, ...patch }));
}

export async function removeShot(id: string): Promise<void> {
  await tx("readwrite", (s) => s.delete(id));
}

/**
 * 启动时调用,做两件事:
 *
 * 1) 把上次残留的 uploading 复位成 pending —— 否则页面在上传中途被关掉,
 *    那几张会永远卡在"上传中",再也不会被重试。
 *
 * 2) 补齐早期版本写入的记录缺失的字段。这是真实踩过的坑:
 *    结构后来加了 nextRetryAt / size / retries,而旧记录里是 undefined,
 *    于是 `undefined <= now` 恒为 false,那些照片永远选不中、传不上去,
 *    体积统计也变成 NaN。给结构加字段就必须迁移已存在的数据。
 *
 * @returns 修复的条数
 */
export async function recoverStaleUploads(): Promise<number> {
  const all = await listShots();
  let fixed = 0;
  for (const s of all) {
    const patch: Partial<PendingShot> = {};
    if (s.status === "uploading") {
      patch.status = "pending";
      patch.nextRetryAt = 0;
    }
    if (typeof s.nextRetryAt !== "number" || Number.isNaN(s.nextRetryAt))
      patch.nextRetryAt = 0;
    if (typeof s.retries !== "number" || Number.isNaN(s.retries))
      patch.retries = 0;
    if (typeof s.size !== "number" || Number.isNaN(s.size))
      patch.size = s.blob?.size ?? 0;
    if (!s.status) patch.status = "pending";
    if (!s.idempotencyKey) patch.idempotencyKey = newId("idem");
    if (!s.capturedAt) patch.capturedAt = new Date().toISOString();

    if (Object.keys(patch).length > 0) {
      await updateShot(s.id, patch);
      fixed += 1;
    }
  }
  return fixed;
}

/** 批量删除。逐条删而非 clear:只删选中的,不误伤还在排队上传的 */
export async function removeShots(ids: string[]): Promise<number> {
  let n = 0;
  for (const id of ids) {
    await removeShot(id);
    n += 1;
  }
  return n;
}

/** 清空(退出登录时用,避免换人后看到上一个人的照片) */
export async function clearShots(): Promise<void> {
  await tx("readwrite", (s) => s.clear());
}
