import ArcoAvatar from "@arco-design/mobile-react/esm/avatar";
import "@arco-design/mobile-react/esm/avatar/style/css";
import { useState } from "react";

import { avatarURL } from "@/lib/avatar";

// ===== Avatar 适配层 =====
//
// 两种形态,同一个组件:
//   有自定义头像 → 显示图片(src)
//   没有         → 用姓名首字生成文字头像(textAvatar)
//
// 回落很重要:大多数巡检员不会主动去传头像,如果没头像就显示一个灰色占位人形,
// 一屏全是一样的灰人;首字头像至少能把人区分开(共用手机时尤其有用)。

export interface AvatarProps {
  /** 显示名;没有图片时取首字做文字头像 */
  name: string;
  /** storage 相对路径(CurrentUser.avatar),空则回落文字 */
  src?: string;
  size?: "ultra-small" | "smaller" | "small" | "medium" | "large";
  className?: string;
}

function initial(name: string): string {
  const s = name.trim();
  if (!s) return "?";
  const first = s[0];
  // 英文/数字统一大写,中文原样
  return /[a-zA-Z]/.test(first) ? first.toUpperCase() : first;
}

export function Avatar({ name, src, size = "ultra-small", className }: AvatarProps) {
  const url = avatarURL(src);
  // 图片取不到时回落文字头像。
  //
  // 这不是纸面上的边界情况:换头像时后端会删掉该用户的旧文件,而其他设备
  // 上仍缓存着旧路径的页面会拿到 404。没有这个回落就是一个裂图。
  // 用 url 作 key,是为了让换头像后重新挂载、清掉上一张的失败状态。
  const [broken, setBroken] = useState(false);
  const showPhoto = Boolean(url) && !broken;

  return (
    <ArcoAvatar
      key={url || "text"}
      className={showPhoto ? `${className || ""} has-photo`.trim() : className}
      size={size}
      shape="circle"
      {...(showPhoto
        ? { src: url as string, imageProps: { onError: () => setBroken(true) } }
        : { textAvatar: initial(name) })}
    />
  );
}
