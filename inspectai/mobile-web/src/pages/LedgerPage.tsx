import { FilterBar, Skeleton, Sticky } from "@/ui";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import EmptyState from "@/components/EmptyState";
import AssetRow from "@/components/AssetRow";
import FlowHeader from "@/components/FlowHeader";
import SectionHeader from "@/components/SectionHeader";
import { AssetDTO, listAssets } from "@/api/inspection";
import { useResource } from "@/hooks/useResource";

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

/**
 * 设备封面缩略图地址。照搬旧版 thumbPath + storageURL 的口径:
 * 后端给的是磁盘路径(Windows 上是 `..\storage\assets\...` 反斜杠),
 * 统一斜杠后截掉 `/storage/` 之前的部分,再拼回 /storage/ 前缀。
 */
function coverURL(a: AssetDTO): string | null {
  const raw = (a.coverImage?.path || "").replace(/\\/g, "/");
  if (!raw) return null;
  const i = raw.indexOf("/storage/");
  if (i < 0) return null;
  return "/storage/" + encodeURI(raw.substring(i + "/storage/".length));
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
  const summary = data?.summary ?? null;

  const stats = useMemo(() => {
    const today = todayStr();
    // 需跟进口径与旧版一致:异常 + 待复核 + 待维修;未巡检不算需跟进
    const risk = summary
      ? (summary.warning || 0) + (summary.danger || 0) + (summary.repair || 0)
      : assets.filter((a) =>
          ["异常", "待复核", "待维修"].includes(a.lastStatus),
        ).length;
    return {
      total: summary?.total ?? assets.length,
      normal:
        summary?.normal ?? assets.filter((a) => a.lastStatus === "正常").length,
      risk,
      today: assets.filter(
        (a) => (a.lastInspectedAt || "").slice(0, 10) === today,
      ).length,
    };
  }, [assets, summary]);

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

  // 分组来源:后端 summary 有就用,没有就从资产现算,不至于筛选整个不可用
  const projects = useMemo(() => {
    if (summary?.projects?.length) return summary.projects;
    const m = new Map<string, number>();
    assets.forEach(
      (a) => a.project && m.set(a.project, (m.get(a.project) || 0) + 1),
    );
    return [...m].map(([value, count]) => ({ value, count }));
  }, [assets, summary]);

  const types = useMemo(() => {
    if (summary?.assetTypes?.length) return summary.assetTypes;
    const m = new Map<string, number>();
    assets.forEach(
      (a) => a.assetType && m.set(a.assetType, (m.get(a.assetType) || 0) + 1),
    );
    return [...m].map(([value, count]) => ({ value, count }));
  }, [assets, summary]);

  const hasFilter = Boolean(
    filter.project || filter.assetType || filter.level || filter.today,
  );

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
        <div className={hasFilter ? "lo-row filtering" : "lo-row"}>
          <button className="lo-card" onClick={() => setFilter({})}>
            <span className="lo-num">{stats.total}</span>
            {/* total 含未巡检设备,写"已巡设备"会和列表里的未巡检行自相矛盾 */}
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
                  setFilter((cur) => ({ ...cur, project: v || undefined })),
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
