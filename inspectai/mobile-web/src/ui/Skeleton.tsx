import ArcoSkeleton from "@arco-design/mobile-react/esm/skeleton";
import "@arco-design/mobile-react/esm/skeleton/style/css";

// ===== Skeleton 适配层 =====
//
// 替掉「一个 20px 转圈孤零零悬在空屏中间」—— 那是整个 app 视觉最弱的时刻:
// 用户不知道要等什么、等多久,页面像坏了。
//
// 骨架屏的价值不是好看,是【页面结构立刻出现】:标题在哪、有几行、每行长什么样,
// 数据到了直接填进去,视觉上没有跳变。
//
// 用 breath(呼吸)不用 gradient(扫光):扫光在一屏铺满卡片时会显得很吵,
// 巡检员多在户外强光下看,低频呼吸更稳。

export interface SkeletonProps {
  /** 列表行数 */
  rows?: number;
  /** 每行前面是否有方块(台账的设备封面图位) */
  avatar?: boolean;
  className?: string;
}

export function Skeleton({
  rows = 5,
  avatar = false,
  className,
}: SkeletonProps) {
  return (
    <div className={className}>
      {Array.from({ length: rows }, (_, i) => (
        <ArcoSkeleton
          key={i}
          className="app-skeleton-row"
          avatar={avatar}
          title={{ width: i % 2 ? "48%" : "62%" }}
          paragraph={{ rows: 1, width: "80%" }}
          animation="breath"
          showAnimation
        />
      ))}
    </div>
  );
}
