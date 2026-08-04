// ===== 拍照辅助:压缩与定位 =====
// 压缩参数沿用旧版 frontend/(长边 1600 / 质量 0.82 / 小于 500KB 不折腾),
// 保证新旧两端上传给 AI 的图质一致,识别表现不产生差异。

const COMPRESS_MAX_EDGE = 1600;
const COMPRESS_QUALITY = 0.82;
const COMPRESS_SKIP_BYTES = 500 * 1024;

async function decodeImage(
  file: Blob,
): Promise<ImageBitmap | HTMLImageElement> {
  if (typeof createImageBitmap === "function") {
    try {
      return await createImageBitmap(file);
    } catch {
      /* 某些 WebView 不支持,回落到 <img> */
    }
  }
  const url = URL.createObjectURL(file);
  try {
    return await new Promise<HTMLImageElement>((resolve, reject) => {
      const img = new Image();
      img.onload = () => resolve(img);
      img.onerror = reject;
      img.src = url;
    });
  } finally {
    URL.revokeObjectURL(url);
  }
}

/**
 * 压缩到长边 1600。压完反而更大(或失败)就退回原图 —— 宁可多传几百 KB,
 * 也不能因为压缩坏了证据照片。
 */
export async function compressImage(file: File): Promise<File> {
  if (!/^image\//.test(file.type) || file.size <= COMPRESS_SKIP_BYTES)
    return file;
  try {
    const bmp = await decodeImage(file);
    const w =
      "width" in bmp ? bmp.width : (bmp as HTMLImageElement).naturalWidth;
    const h =
      "height" in bmp ? bmp.height : (bmp as HTMLImageElement).naturalHeight;
    if (!w || !h) return file;

    const ratio = Math.min(1, COMPRESS_MAX_EDGE / Math.max(w, h));
    const canvas = document.createElement("canvas");
    canvas.width = Math.round(w * ratio);
    canvas.height = Math.round(h * ratio);
    canvas
      .getContext("2d")
      ?.drawImage(bmp as CanvasImageSource, 0, 0, canvas.width, canvas.height);
    if ("close" in bmp && typeof bmp.close === "function") bmp.close();

    const blob = await new Promise<Blob | null>((resolve) =>
      canvas.toBlob(resolve, "image/jpeg", COMPRESS_QUALITY),
    );
    if (!blob || blob.size >= file.size) return file;
    return new File([blob], file.name.replace(/\.[^.]+$/, "") + ".jpg", {
      type: "image/jpeg",
    });
  } catch {
    return file;
  }
}

/**
 * 取当前定位。拿不到就返回 null —— 定位失败绝不能挡住拍照,
 * 现场信号差本来就是常态。
 */
export function getGeo(
  timeoutMs = 8000,
): Promise<{ lat: number; lng: number; accuracy: number } | null> {
  return new Promise((resolve) => {
    if (!navigator.geolocation) {
      resolve(null);
      return;
    }
    navigator.geolocation.getCurrentPosition(
      (pos) =>
        resolve({
          lat: pos.coords.latitude,
          lng: pos.coords.longitude,
          accuracy: pos.coords.accuracy,
        }),
      () => resolve(null),
      {
        // 【不要 enableHighAccuracy】那会强制走 GPS,而巡检现场是机房、
        // 泵房、变电所 —— 看不到天空,正是 GPS 最慢最容易失败的场景,
        // 常常跑满超时才回来。关掉后走基站/WiFi 定位,通常一两秒内出结果。
        // 这里要回答的问题是"这张照片是在哪栋楼拍的",百米级精度足够;
        // 米级精度既拿不到,也没用。
        enableHighAccuracy: false,
        timeout: timeoutMs,
        // 同一次巡检里连拍多张,一分钟内复用上次结果,不重复定位
        maximumAge: 60_000,
      },
    );
  });
}
