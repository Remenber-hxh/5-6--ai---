import { Toast } from "antd-mobile";
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

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
  if (Number.isNaN(d.getTime())) return "未巡检";
  return `${d.getMonth() + 1}/${d.getDate()}`;
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

  if (loading) {
    return (
      <div className="center-screen">
        <span className="spinner" />
      </div>
    );
  }

  return (
    <div className="flow-screen">
      <div className="flow-head">
        <h1 className="flow-title">设备健康</h1>
        <p className="flow-sub">共 {stats.total} 台设备</p>
      </div>

      <div className="scroll-area flow-body">
        {/* 概览四数,点击即筛选 */}
        <div className="lo-row">
          <button className="lo-card" onClick={() => setFilter({})}>
            <span className="lo-num">{stats.total}</span>
            <span className="lo-label">已巡设备</span>
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
            <div className="task-empty">没有符合条件的设备</div>
          ) : (
            shown.map((a) => {
              const t = tone(a.lastStatus);
              return (
                <button className="asset-row" key={a.id} onClick={() => nav(`/asset/${encodeURIComponent(a.id)}`)}>
                  <span className={`ar-dot ar-${t.cls}`} />
                  <span className="ar-main">
                    <span className="ar-name">{a.assetName}</span>
                    <span className="ar-sub">
                      {a.project || "—"} · 巡检 {a.inspectionCount} 次 · {fmtDate(a.lastInspectedAt)}
                    </span>
                  </span>
                  <span className={`ar-tag ar-${t.cls}`}>{t.text}</span>
                </button>
              );
            })
          )}
        </div>
      </div>

      <div className="flow-foot">
        <button className="task-back" onClick={() => nav("/")}>
          返回拍照
        </button>
      </div>
    </div>
  );
}
