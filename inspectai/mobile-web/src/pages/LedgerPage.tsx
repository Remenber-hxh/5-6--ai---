import { Skeleton, Toast } from "@/ui";
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import FlowHeader from "@/components/FlowHeader";
import { AssetDTO, AssetSummary, listAssets } from "@/api/inspection";

/** 状态 → 视觉档位。与旧版 statusLevel 口径一致 */
function tone(status: string): { cls: string; text: string } {
  switch (status) {
    case "正常":
      return { cls: "ok", text: "正常" };
    case "异常":
      return { cls: "danger", text: "异常" };
    case "待复核":
      return { cls: "warn", text: "待复核" };
    case "待维修":
      return { cls: "repair", text: "待维修" };
    default:
      return { cls: "unknown", text: status || "未巡检" };
  }
}

/** 本地日历日。lastInspectedAt 带时区,用本地日避免凌晨把"今日"算到昨天 */
function todayStr(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
}

function fmtDate(iso?: string): string {
  if (!iso) return "未巡检";
  const d = new Date(iso);
  // Go 零值时间(0001-01-01)解析出来年份是 1,之前没防 —— 从未巡检的设备
  // 行尾全挂着一个"12/31"。年份异常一律按未巡检处理。
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return "未巡检";
  return `${d.getMonth() + 1}/${d.getDate()}`;
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

type Filter = { project?: string; assetType?: string; level?: string; today?: boolean };

// 设备健康(旧版 sceneLedger):概览四数 + 分组筛选 + 资产列表
export default function LedgerPage() {
  const nav = useNavigate();
  const [assets, setAssets] = useState<AssetDTO[]>([]);
  const [summary, setSummary] = useState<AssetSummary | null>(null);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState<Filter>({});

  useEffect(() => {
    void (async () => {
      try {
        const data = await listAssets();
        setAssets(data.assets);
        setSummary(data.summary);
      } catch {
        Toast.show({ content: "台账加载失败" });
      } finally {
        setLoading(false);
      }
    })();
  }, []);

  const stats = useMemo(() => {
    const today = todayStr();
    // 需跟进口径与旧版一致:异常 + 待复核 + 待维修;未巡检不算需跟进
    const risk = summary
      ? (summary.warning || 0) + (summary.danger || 0) + (summary.repair || 0)
      : assets.filter((a) => ["异常", "待复核", "待维修"].includes(a.lastStatus)).length;
    return {
      total: summary?.total ?? assets.length,
      normal: summary?.normal ?? assets.filter((a) => a.lastStatus === "正常").length,
      risk,
      today: assets.filter((a) => (a.lastInspectedAt || "").slice(0, 10) === today).length,
    };
  }, [assets, summary]);

  const shown = useMemo(() => {
    const today = todayStr();
    return assets.filter((a) => {
      if (filter.project && a.project !== filter.project) return false;
      if (filter.assetType && a.assetType !== filter.assetType) return false;
      if (filter.today && (a.lastInspectedAt || "").slice(0, 10) !== today) return false;
      if (filter.level === "normal" && a.lastStatus !== "正常") return false;
      if (filter.level === "risk" && !["异常", "待复核", "待维修"].includes(a.lastStatus)) return false;
      return true;
    });
  }, [assets, filter]);

  // 分组来源:后端 summary 有就用,没有就从资产现算,不至于筛选整个不可用
  const projects = useMemo(() => {
    if (summary?.projects?.length) return summary.projects;
    const m = new Map<string, number>();
    assets.forEach((a) => a.project && m.set(a.project, (m.get(a.project) || 0) + 1));
    return [...m].map(([value, count]) => ({ value, count }));
  }, [assets, summary]);

  const types = useMemo(() => {
    if (summary?.assetTypes?.length) return summary.assetTypes;
    const m = new Map<string, number>();
    assets.forEach((a) => a.assetType && m.set(a.assetType, (m.get(a.assetType) || 0) + 1));
    return [...m].map(([value, count]) => ({ value, count }));
  }, [assets, summary]);

  const hasFilter = Boolean(filter.project || filter.assetType || filter.level || filter.today);

  /** 点同一项 = 取消选择,不用另找清除按钮 */
  function pick<K extends keyof Filter>(key: K, value: Filter[K]) {
    setFilter((cur) => ({ ...cur, [key]: cur[key] === value ? undefined : value }));
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
        <p className="flow-caption">共 {stats.total} 台设备</p>
        {/* 概览四数,点击即筛选 */}
        <div className="lo-row">
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

        {projects.length > 1 && (
          <div className="lg-card">
            <div className="lg-title">项目</div>
            <div className="lg-chips">
              {projects.map((g) => (
                <button
                  key={g.value}
                  className={filter.project === g.value ? "lg-chip on" : "lg-chip"}
                  onClick={() => pick("project", g.value)}
                >
                  {g.value} <em>{g.count}</em>
                </button>
              ))}
            </div>
          </div>
        )}

        {types.length > 1 && (
          <div className="lg-card">
            <div className="lg-title">设备类型</div>
            <div className="lg-chips">
              {types.map((g) => (
                <button
                  key={g.value}
                  className={filter.assetType === g.value ? "lg-chip on" : "lg-chip"}
                  onClick={() => pick("assetType", g.value)}
                >
                  {g.value} <em>{g.count}</em>
                </button>
              ))}
            </div>
          </div>
        )}

        {hasFilter && (
          <button className="lg-clear" onClick={() => setFilter({})}>
            清除筛选 · 当前 {shown.length} 台
          </button>
        )}

        <div className="asset-list">
          {shown.length === 0 ? (
            <div className="empty-state">
              <span className="es-badge">∅</span>
              <span className="es-title">没有符合条件的设备</span>
              <span className="es-hint">{hasFilter ? "点上方「清除筛选」再看全部" : "还没有设备数据"}</span>
            </div>
          ) : (
            shown.map((a) => {
              const t = tone(a.lastStatus);
              const cover = coverURL(a);
              return (
                <button className="asset-row" key={a.id} onClick={() => nav(`/asset/${encodeURIComponent(a.id)}`)}>
                  {/* 旧版每行有设备封面图,重构时丢了。状态由右侧标签表达,不再另画圆点 */}
                  {cover ? (
                    <img className="ar-cover" src={cover} alt="" loading="lazy" />
                  ) : (
                    <span className="ar-cover ar-cover-empty" aria-hidden />
                  )}
                  <span className="ar-main">
                    <span className="ar-name">{a.assetName}</span>
                    <span className="ar-sub">
                      {a.project || "—"} · 巡检 {a.inspectionCount} 次 · {fmtDate(a.lastInspectedAt)}
                    </span>
                  </span>
                  <span className={`tag tag-${t.cls}`}>{t.text}</span>
                </button>
              );
            })
          )}
        </div>
      </div>
    </div>
  );
}
