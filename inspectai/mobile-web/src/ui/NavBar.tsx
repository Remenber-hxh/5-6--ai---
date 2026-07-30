import ArcoNavBar from "@arco-design/mobile-react/esm/nav-bar";
import "@arco-design/mobile-react/esm/nav-bar/style/css";
import type { ReactNode } from "react";

// ===== NavBar 适配层 =====
//
// 旧版有统一顶栏(frontend/index.html 的 #topbar):返回「‹」+ 左对齐标题 + 右侧操作位,
// 并在 camera / loading 两屏换成深色 + 翡翠强调色。新版重构时把它整个丢了 ——
// 每页各写一个 .flow-head,没有返回入口,只能靠底部一整条大按钮走,既不符合
// 移动端习惯也白占底部空间。这里补回来。
//
// 两点与 Arco 默认行为不同,踩过要记住:
//   1. Arco 的 `title` 是【居中】的(带溢出省略);旧版是左对齐 ——
//      故走 `children` 槽自己排版。children 只替换中间块,左右两侧照常渲染。
//   2. 必须显式 `fixed={false}` —— 它【默认是 true】。默认下 wrapper 会
//      position:fixed 贴 window,外层只留一个高 0.88rem 的占位。我们把栏高改成
//      旧版的 56px 后,占位比真实高度矮 10px,正文顶部会被压掉 10px。
//      本项目是 .flow-screen 内部滚动(.scroll-area 自己滚),NavBar 作为 flex
//      首个子元素天然就吸顶,不需要它的 fixed 定位。

export interface NavBarProps {
  title: ReactNode;
  /** 返回回调。不传则不渲染返回键(拍照台、登录页 —— 对齐旧版 backBtn.hidden) */
  onBack?: () => void;
  /** 右侧操作(旧版 #topAction,例如「设备健康」) */
  action?: { text: string; onClick: () => void };
  /** 深色科技风:拍照台 / 识别中(旧版 data-scene="camera|loading" 的覆盖) */
  dark?: boolean;
}

export function NavBar({ title, onBack, action, dark }: NavBarProps) {
  return (
    <ArcoNavBar
      fixed={false}
      wrapClass={dark ? "app-navbar app-navbar-dark" : "app-navbar"}
      // undefined = 用 Arco 自带的返回箭头 SVG(currentColor,比旧版的「‹」字形干净)
      leftContent={onBack ? undefined : null}
      onClickLeft={onBack}
      rightContent={action ? <span className="navbar-action">{action.text}</span> : null}
      onClickRight={action?.onClick}
    >
      <div className="navbar-title">{title}</div>
    </ArcoNavBar>
  );
}
