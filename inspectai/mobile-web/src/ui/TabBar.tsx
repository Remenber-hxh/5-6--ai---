import ArcoTabBar from "@arco-design/mobile-react/esm/tab-bar";
import "@arco-design/mobile-react/esm/tab-bar/style/css";
import type { ReactNode } from "react";

// ===== TabBar 适配层 =====
//
// 这一层做的事很少 —— Arco 的 TabBar 平台分支为 0(不像 dialog 有 52 条),
// 尺寸也不吃 rem 陷阱,所以主要是把 dataSource 那套换成更直白的 items 数组,
// 并把"角标"这件事从调用方手里接过来。
//
// 注意不要用它的 fixed:本项目是 .app-shell 的 flex 列布局,TabBar 作为
// 最后一个 flex 子元素天然贴底,用 position:fixed 反而要另算占位高度
// (NavBar 就是在这上面踩过 —— 占位比真实高度矮 10px,正文顶部被压掉一截)。

export interface TabItem {
  key: string;
  title: string;
  /** 传函数可按选中态换图标;这里统一用 currentColor,选中色交给 CSS */
  icon: ReactNode;
  /** 右上角角标数字,0 或不传则不显示 */
  badge?: number;
}

export interface TabBarProps {
  items: TabItem[];
  /** 当前选中项的 key;不在 items 里则不高亮任何一项 */
  activeKey: string;
  onChange: (key: string) => void;
}

export function TabBar({ items, activeKey, onChange }: TabBarProps) {
  const activeIndex = items.findIndex((i) => i.key === activeKey);

  return (
    <ArcoTabBar
      className="app-tabbar"
      // 【必须显式 false】tab-bar.js:19 里默认是 true —— 会加上
      // .arco-tab-bar-fixed(position:fixed; bottom:0),脱离文档流盖住页面
      // 底部内容(个人页的「退出登录」当场被盖住)。
      // 本项目 .app-shell 是 flex 列,TabBar 作为最后一个子元素天然贴底,
      // 用 fixed 反而要另算占位高度。NavBar 也踩过同一个默认值。
      fixed={false}
      // -1(当前路由不是任何一个 tab)时 Arco 会当成未选中,正是想要的
      activeIndex={activeIndex}
      onChange={(index) => {
        const item = items[index];
        if (item) onChange(item.key);
      }}
      dataSource={items.map((it) => ({
        title: it.title,
        icon: (
          <span className="tb-icon">
            {it.icon}
            {/* 角标自己写:Arco TabBar 的 extra 槽在标题右侧,而移动端习惯是
                图标右上角。数字大于 99 收成 99+,否则会把整个 tab 撑宽。 */}
            {it.badge ? (
              <em className="tb-badge">{it.badge > 99 ? "99+" : it.badge}</em>
            ) : null}
          </span>
        ),
      }))}
    />
  );
}
