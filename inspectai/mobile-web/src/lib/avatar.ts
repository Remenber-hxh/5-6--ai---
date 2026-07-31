// ===== 头像:客户端压缩 + 上传 =====
//
// 为什么必须在客户端压:手机相册随手一张就是 3–8MB,直接传在弱网现场
// 要等半天,后端 2MB 上限也会直接拒。压到 256px 方形后通常 20–40KB。
//
// 为什么裁成方形而不是等比缩放:头像位是圆形/方形展示,不裁的话
// 竖构图的人脸会被 object-fit 切掉半张 —— 与其让 CSS 乱切,不如自己
// 按中心裁,至少保证主体居中。

const AVATAR_SIZE = 256;

/** 读文件 → 居中方形裁切 → 缩到 256 → JPEG。返回可直接上传的 Blob。 */
export async function prepareAvatar(file: File): Promise<Blob> {
  const bitmap = await loadBitmap(file);
  const side = Math.min(bitmap.width, bitmap.height);
  const sx = (bitmap.width - side) / 2;
  const sy = (bitmap.height - side) / 2;

  const canvas = document.createElement("canvas");
  canvas.width = AVATAR_SIZE;
  canvas.height = AVATAR_SIZE;
  const ctx = canvas.getContext("2d");
  if (!ctx) throw new Error("当前浏览器不支持图片处理");
  ctx.drawImage(bitmap, sx, sy, side, side, 0, 0, AVATAR_SIZE, AVATAR_SIZE);
  // 用完释放,ImageBitmap 不释放会占着解码后的位图内存
  if ("close" in bitmap) bitmap.close();

  const blob = await new Promise<Blob | null>((resolve) =>
    canvas.toBlob(resolve, "image/jpeg", 0.85),
  );
  if (!blob) throw new Error("图片处理失败");
  return blob;
}

async function loadBitmap(file: File): Promise<ImageBitmap> {
  // createImageBitmap 在企微 webview 上不一定有,回退到 <img>
  if (typeof createImageBitmap === "function") {
    try {
      return await createImageBitmap(file);
    } catch {
      /* 落到下面的兜底 */
    }
  }
  const url = URL.createObjectURL(file);
  try {
    const img = await new Promise<HTMLImageElement>((resolve, reject) => {
      const el = new Image();
      el.onload = () => resolve(el);
      el.onerror = () => reject(new Error("图片读取失败"));
      el.src = url;
    });
    // 用 canvas 转一手,拿到统一的 ImageBitmap 接口
    return await createImageBitmapFallback(img);
  } finally {
    URL.revokeObjectURL(url);
  }
}

function createImageBitmapFallback(img: HTMLImageElement): Promise<ImageBitmap> {
  // 没有 createImageBitmap 时,drawImage 直接吃 HTMLImageElement 也行 ——
  // 这里包一层同形状的对象,让上面的代码不用分支
  return Promise.resolve({
    width: img.naturalWidth,
    height: img.naturalHeight,
    close: () => void 0,
    // drawImage 接受 CanvasImageSource,HTMLImageElement 也是其中之一
    ...(img as unknown as object),
  } as unknown as ImageBitmap);
}

/** 头像相对路径 → 可访问 URL。空值返回 null,调用方回落文字头像。 */
export function avatarURL(path?: string): string | null {
  const p = (path || "").trim();
  if (!p) return null;
  // 已经是完整 URL(未来可能接 CDN)就原样用
  if (/^https?:\/\//i.test(p)) return p;
  return "/storage/" + encodeURI(p.replace(/^\/+/, ""));
}
