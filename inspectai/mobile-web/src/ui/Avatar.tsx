import ArcoAvatar from "@arco-design/mobile-react/esm/avatar";
import "@arco-design/mobile-react/esm/avatar/style/css";

// ===== Avatar 适配层 =====
//
// 首页右上角的身份钮原来是一段纯文字。加个文字头像后有了"这是个人"的锚点,
// 也让顶栏左右两端的视觉重量配平(左边是产品名,右边只有文字会显得飘)。
//
// 系统没有头像图,用 textAvatar 取姓名首字 —— 中文取第 1 个字,
// 英文用户名取首字母大写。

export interface AvatarProps {
  /** 显示名;取首字做头像 */
  name: string;
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

export function Avatar({ name, size = "ultra-small", className }: AvatarProps) {
  return <ArcoAvatar className={className} size={size} shape="circle" textAvatar={initial(name)} />;
}
