import { FilterBar, Skeleton, Sticky } from "@/ui";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import EmptyState from "@/components/EmptyState";
import AssetRow from "@/components/AssetRow";
import FlowHeader from "@/components/FlowHeader";
import SectionHeader from "@/components/SectionHeader";
import { AssetDTO, listAssets } from "@/api/inspection";
import { useResource } from "@/hooks/useResource";
import { coverURL } from "@/lib/assetCover";

/**
 * 列表分组:需跟进的排最前。
 *
 * 原来是一条平铺的列表,34 台设备里那 3 台"需跟进"的散在中间 —— 管理者
 * 进这一页就是来找它们的,却要一行行扫。分组 + 吸顶标题后,滚到哪都知道
 * 当前在哪一组。
 *
 * 色档统一由 StatusTag 判(见 components/StatusTag.tsx),这里只管分组。
 */
const RISK_STATUS = ["异常", "待复核", "待维修"];

function groupAssets(list: AssetDTO[]): {
  key: string;
  title: string;
  tone: "risk" | "ok" | "muted";
  items: AssetDTO[];
}[] {
  const risk: AssetDTO[] = [];
  const ok: AssetDTO[] = [];
  const never: AssetDTO[] = [];
  for (const a of list) {
    if (RISK_STATUS.includes(a.lastStatus)) risk.push(a);
    else if (a.lastStatus === "正常") ok.push(a);
    else never.push(a);
  }
  // tone 决定分组标题那个圆点的颜色 —— 一页两三组,靠颜色定位比读字快
  return [
    { key: "risk", title: "需跟进", tone: "risk" as const, items: risk },
    { key: "ok", title: "健康", tone: "ok" as const, items: ok },
    { key: "never", title: "未巡检", tone: "muted" as const, items: never },
  ].filter((g) => g.items.length > 0);
}

/** 本地日历日。lastInspectedAt 带时区,用本地日避免凌晨把"今日"算到昨天 */
function todayStr(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

type Filter = {
  project?: string;
  assetType?: string;
  level?: string;
  today?: boolean;
};

// 设备健康(旧版 sceneLedger):概览四数 + 分组筛选 + 资产列表
export default function LedgerPage() {
  const nav = useNavigate();
  const [filter, setFilter] = useState<Filter>({});

  // useResource 负责竞态防护、卸载保护和错误提示 —— 原来这里是裸的
  // useEffect + try/catch,慢响应回来会盖掉新状态(见 hooks/useResource.ts)
  const { data, loading } = useResource((signal) => listAssets(signal), [], {
    errorText: "台账加载失败",
  });
  const assets = data?.assets ?? [];

  // 【四个数跟着项目/类型走】选了「紫菡雅集」,下面列的是 15 台,上面却还写着
  // 38 台、32 台健康 —— 一屏两个数字对不上,人不知道该信哪个。
  // 项目和类型是"看哪一片",四张卡是"这一片里挑哪一档";先圈范围再数。
  const scoped = useMemo(
    () =>
      assets.filter(
        (a) =>
          (!filter.project || a.project === filter.project) &&
          (!filter.assetType || a.assetType === filter.assetType),
      ),
    [assets, filter.project, filter.assetType],
  );

  const stats = useMemo(() => {
    const today = todayStr();
    // 需跟进口径与旧版一致:异常 + 待复核 + 待维修;未巡检不算需跟进。
    // 【直接数列表,不用后端 summary】summary 数的是全部设备,圈了范围之后
    // 和下面的列表不是同一批 —— 同一屏的数字必须同源。
    return {
      total: scoped.length,
      normal: scoped.filter((a) => a.lastStatus === "正常").length,
      risk: scoped.filter((a) => RISK_STATUS.includes(a.lastStatus)).length,
      today: scoped.filter(
        (a) => (a.lastInspectedAt || "").slice(0, 10) === today,
      ).length,
    };
  }, [scoped]);

  const shown = useMemo(() => {
    const today = todayStr();
    return assets.filter((a) => {
      if (filter.project && a.project !== filter.project) return false;
      if (filter.assetType && a.assetType !== filter.assetType) return false;
      if (filter.today && (a.lastInspectedAt || "").slice(0, 10) !== today)
        return false;
      if (filter.level === "normal" && a.lastStatus !== "正常") return false;
      if (
        filter.level === "risk" &&
        !["异常", "待复核", "待维修"].includes(a.lastStatus)
      )
        return false;
      return true;
    });
  }, [assets, filter]);

  // 选项和台数都从列表现数,理由同上:面板里写「电表 7」,点进去就得是 7 台。
  function countBy(list: AssetDTO[], key: "project" | "assetType") {
    const m = new Map<string, number>();
    list.forEach((a) => a[key] && m.set(a[key], (m.get(a[key]) || 0) + 1));
    return [...m]
      .sort((x, y) => y[1] - x[1])
      .map(([value, count]) => ({ value, count }));
  }
  const projects = useMemo(() => countBy(assets, "project"), [assets]);
  // 【类型只列这个项目里有的】选了紫菡雅集,面板里再摆「有机房电梯 10」
  // 就是一个点了必然为空的选项。
  const types = useMemo(
    () =>
      countBy(
        filter.project ? assets.filter((a) => a.project === filter.project) : assets,
        "assetType",
      ),
    [assets, filter.project],
  );

  const hasFilter = Boolean(
    filter.project || filter.assetType || filter.level || filter.today,
  );
  // 四张卡的"其余变淡"只看卡自己那一档 —— 选了项目时四张卡一起变淡,
  // 看着像整块失效了,其实它们只是在数这个项目。
  const tileFiltering = Boolean(filter.level || filter.today);

  /** 点同一项 = 取消选择,不用另找清除按钮 */
  function pick<K extends keyof Filter>(key: K, value: Filter[K]) {
    setFilter((cur) => ({
      ...cur,
      [key]: cur[key] === value ? undefined : value,
    }));
  }

  // 加载态保留顶栏 + 骨架列表:页面结构立刻出现,数据到了直接填进去,
  // 不再是一个转圈孤零零悬在空屏中间。avatar 位对应设备封面缩略图。
  if (loading) {
    return (
      <div className="flow-screen">
        <FlowHeader title="设备健康" onBack={() => nav("/")} />
        <div className="scroll-area flow-body">
          <Skeleton rows={6} avatar className="sk-list" />
        </div>
      </div>
    );
  }

  return (
    <div className="flow-screen">
      <FlowHeader title="设备健康" onBack={() => nav("/")} />

      <div className="scroll-area flow-body">
        {/* 概览四数,点击即筛选 */}
        <div className={tileFiltering ? "lo-row filtering" : "lo-row"}>
          {/* 没有按档筛时这张卡是选中态 —— 照旧版,表达"当前看的是全部"。
              少了它,四张卡在初始状态下没有一张是亮的,选中态这个语言就没有
              起点,用户点第一下之前不知道亮起来意味着什么。
              点它只退掉"档",不退项目/类型 —— 那是下面筛选栏管的。 */}
          <button
            className={tileFiltering ? "lo-card" : "lo-card on"}
            onClick={() =>
              setFilter((cur) => ({ project: cur.project, assetType: cur.assetType }))
            }
          >
            <span className={tileFiltering ? "lo-num" : "lo-num blue"}>
              {stats.total}
            </span>
            {/* 旧版这里写的是"已巡设备",但 total 含未巡检设备(现在有 2 台),
                那个标签和列表里的"未巡检"分组自相矛盾。保留正确的说法。 */}
            <span className="lo-label">设备总数</span>
          </button>
          <button
            className={filter.level === "normal" ? "lo-card on" : "lo-card"}
            onClick={() => pick("level", "normal")}
          >
            <span className="lo-num ok">{stats.normal}</span>
            <span className="lo-label">健康</span>
          </button>
          <button
            className={filter.level === "risk" ? "lo-card on" : "lo-card"}
            onClick={() => pick("level", "risk")}
          >
            <span className="lo-num warn">{stats.risk}</span>
            <span className="lo-label">需跟进</span>
          </button>
          <button
            className={filter.today ? "lo-card on" : "lo-card"}
            onClick={() => pick("today", filter.today ? undefined : true)}
          >
            <span className="lo-num blue">{stats.today}</span>
            <span className="lo-label">今日已巡</span>
          </button>
        </div>

        {/* 筛选栏:组件库的 DropdownMenu(见 ui/FilterBar.tsx)。
            之前是折叠面板里塞两排药丸片,11 个设备类型换行成 4 排、展开后
            占大半屏,而"折叠面板"这个形态传达的是"这里有一块内容"而不是
            "这里可以筛"。 */}
        {(projects.length > 1 || types.length > 1) && (
          <FilterBar
            groups={[
              {
                label: "项目",
                options: projects.map((g) => ({
                  value: g.value,
                  count: g.count,
                })),
                value: filter.project ?? "",
                onChange: (v) =>
                  setFilter((cur) => ({
                    ...cur,
                    project: v || undefined,
                    // 换了项目,原来选的类型在新项目里没有的话就放掉 ——
                    // 否则列表是空的,而栏上看不出为什么
                    assetType:
                      cur.assetType &&
                      (!v || assets.some((a) => a.project === v && a.assetType === cur.assetType))
                        ? cur.assetType
                        : undefined,
                  })),
              },
              {
                label: "设备类型",
                options: types.map((g) => ({ value: g.value, count: g.count })),
                value: filter.assetType ?? "",
                onChange: (v) =>
                  setFilter((cur) => ({ ...cur, assetType: v || undefined })),
              },
            ]}
          />
        )}

        {hasFilter && (
          <button className="lg-clear" onClick={() => setFilter({})}>
            清除筛选 · 当前 {shown.length} 台
          </button>
        )}

        <div className="asset-list">
          {shown.length === 0 ? (
            <EmptyState
              title="没有符合条件的设备"
              hint={hasFilter ? "点上方「清除筛选」再看全部" : "还没有设备数据"}
            />
          ) : (
            groupAssets(shown).map((g) => (
              <section className="asset-group" key={g.key}>
                <Sticky topOffset={0}>
                  <SectionHeader
                    title={g.title}
                    count={g.items.length}
                    tone={g.tone}
                  />
                </Sticky>
                <div className="asset-card">
                  {g.items.map((a) => (
                    <AssetRow
                      key={a.id}
                      asset={a}
                      cover={coverURL(a)}
                      onClick={() => nav(`/asset/${encodeURIComponent(a.id)}`)}
                    />
                  ))}
                </div>
              </section>
            ))
          )}
        </div>
      </div>
    </div>
  );
}
