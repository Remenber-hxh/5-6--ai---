import { Image, Toast } from "@/ui";
import { useCallback, useEffect, useState } from "react";

import ChangeRequestSheet from "@/components/ChangeRequestSheet";
import { useNavigate, useParams } from "react-router-dom";

import CenterLoading from "@/components/CenterLoading";
import FlowHeader from "@/components/FlowHeader";
import StatusTag from "@/components/StatusTag";
import {
  AssetSnapshotDTO,
  RecordDTO,
  getAsset,
  getRecord,
  listAssetChangeRequests,
  listAssetRecords,
} from "@/api/inspection";
import { fieldIsBad } from "@/lib/fieldStatus";
import { useResource } from "@/hooks/useResource";
import { useAuth } from "@/store/auth";
import { setRetakeTarget } from "@/store/retake";

function fmtWhen(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getMonth() + 1}/${d.getDate()} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

// 申请状态的中文。后端存的是英文枚举,直接把 "pending" 显示给现场没有意义。
const CR_STATUS: Record<string, string> = {
  pending: "待审批",
  approved: "已通过",
  rejected: "已驳回",
  withdrawn: "已撤回",
};

// 资产详情(旧版 sceneAsset):当前状态 + 巡检历史(按快照分页翻完整历史)
export default function AssetDetailPage() {
  const { id = "" } = useParams();
  const nav = useNavigate();
  const [page, setPage] = useState(1);
  const [crOpen, setCrOpen] = useState(false);
  const [histExpanded, setHistExpanded] = useState(false);
  // 管理角色能看到别人发起的申请,一线只看得到自己的(后端已按此过滤)。
  // 这里只影响标题文案 —— 数据范围不是前端说了算。
  const isMgmt = ["admin", "manager", "supervisor"].includes(
    useAuth.getState().user?.roleCode || "",
  );
  // 展开某条历史时才去取那条巡检记录(要图片和原始字段)。
  // 【不预取】一次取 5 条记录、每条带 images_json 和 fields_json,在机房弱网下
  // 是纯浪费 —— 绝大多数时候用户只会展开一两条。
  const [recCache, setRecCache] = useState<Record<string, RecordDTO>>({});
  const [recBusy, setRecBusy] = useState<Record<string, boolean>>({});

  const loadRecord = useCallback(async (recordId: string) => {
    if (!recordId) return;
    // 已经有了或正在取就别重复发 —— <details> 的 toggle 会被反复触发
    let skip = false;
    setRecBusy((m) => {
      if (m[recordId]) skip = true;
      return m;
    });
    if (skip) return;
    setRecCache((c) => {
      if (c[recordId]) skip = true;
      return c;
    });
    if (skip) return;
    setRecBusy((m) => ({ ...m, [recordId]: true }));
    try {
      const rec = await getRecord(recordId);
      setRecCache((c) => ({ ...c, [recordId]: rec }));
    } catch {
      // 静默:AI 总结那段还在,不至于展开后一片空白。
      // 这条记录可能已被清理(历史数据里有引用已删记录的快照)。
    } finally {
      setRecBusy((m) => ({ ...m, [recordId]: false }));
    }
  }, []);

  // 换设备(id 变)时 useResource 会作废上一次的飞行请求 —— 原来没有这层,
  // 快速连点两台设备可能被先发后到的旧响应盖掉
  const { data, loading } = useResource(
    async (signal) => {
      const [a, rec] = await Promise.all([
        getAsset(id, signal),
        listAssetRecords(id, 1, signal),
      ]);
      return { asset: a, snapshots: rec.snapshots, totalPages: rec.totalPages };
    },
    [id],
    { errorText: "设备信息加载失败" },
  );

  // 首页数据来自 useResource,翻页追加的部分放在本地 —— id 变了要重置
  const [more, setMore] = useState<AssetSnapshotDTO[]>([]);
  useEffect(() => {
    setMore([]);
    setPage(1);
  }, [id]);

  const asset = data?.asset ?? null;

  // 这台设备是不是"还没了结"。判据和台账列表的状态标一致 ——
  // 两处口径分叉的话,列表标红的设备点进去却没有复检入口,人会以为功能坏了。
  // statusLevel 是后端算好的等级,lastStatus 是中文标签;两个都认,
  // 因为历史数据里有只有中文标签、没有 statusLevel 的行。
  const needsFollowup = Boolean(
    asset &&
      (["danger", "warning", "repair"].includes(asset.statusLevel || "") ||
        ["异常", "待复核", "待维修", "待整改"].includes(asset.lastStatus || "")),
  );
  const snaps = [...(data?.snapshots ?? []), ...more];
  // 折叠阈值。旧版是 7,产品改成 5 —— 手机上一屏放不下更多。
  const HISTORY_LIMIT = 5;
  const shownSnaps = histExpanded ? snaps : snaps.slice(0, HISTORY_LIMIT);

  // 第一条默认是展开的,而 <details> 的 onToggle 【不会】为"初始就 open"触发,
  // 所以要单独拉一次 —— 否则最上面那条永远没有照片和字段,而下面的都有,
  // 看起来像"第一条坏了"。
  // 这台设备相关的修改申请。和历史分开取:它是另一条时间线(谁提了什么、批没批),
  // 混进巡检历史里会让"这台设备发生过什么"这条线索变浑。
  const { data: reqs } = useResource(
    (signal) => listAssetChangeRequests(id, signal),
    [id],
    { errorText: null },
  );

  const firstRecordId = shownSnaps[0]?.recordId;
  useEffect(() => {
    if (firstRecordId) void loadRecord(firstRecordId);
  }, [firstRecordId, loadRecord]);
  const totalPages = data?.totalPages ?? 1;

  async function loadMore() {
    const next = page + 1;
    try {
      const rec = await listAssetRecords(id, next);
      setMore((cur) => [...cur, ...rec.snapshots]);
      setPage(next);
    } catch {
      Toast.show({ content: "加载更多失败" });
    }
  }

  if (loading) {
    return <CenterLoading />;
  }
  if (!asset) {
    return (
      <div className="center-screen">
        <p className="screen-sub">设备不存在</p>
        {/* 这是深色屏,不能用浅色页的 .task-back */}
        <button className="btn-dark-ghost" onClick={() => nav("/ledger")}>
          返回台账
        </button>
      </div>
    );
  }

  return (
    <div className="flow-screen">
      <FlowHeader title={asset.assetName} onBack={() => nav("/ledger")} />

      <div className="scroll-area flow-body">
        <p className="flow-caption">
          {asset.project || "—"}
          {asset.assetType ? ` · ${asset.assetType}` : ""}
        </p>
        {/* 当前状态 */}
        <div className="ad-card">
          <div className="ad-row">
            <span className="ad-k">当前状态</span>
            <StatusTag text={asset.lastStatus || "未巡检"} />
          </div>
          <div className="ad-row">
            <span className="ad-k">累计巡检</span>
            <span className="ad-v">{asset.inspectionCount} 次</span>
          </div>
          <div className="ad-row">
            <span className="ad-k">最近巡检</span>
            <span className="ad-v">
              {fmtWhen(asset.lastInspectedAt) || "未巡检"}
            </span>
          </div>
          {asset.lastInspector && (
            <div className="ad-row">
              <span className="ad-k">最近巡检人</span>
              <span className="ad-v">{asset.lastInspector}</span>
            </div>
          )}
          {asset.lastSummary && (
            <div className="ad-summary">{asset.lastSummary}</div>
          )}
        </div>

        {/* 【重新拍照复检】只在这台设备真的需要跟进时出现。
            正常设备摆一个"复检"按钮是噪音,还会诱导出多余的巡检记录。

            这是旧版有、新版一直缺的一条闭环:台账里挂着异常,巡检员现场复查完
            却没有办法把它销掉 —— 直接重拍会生成一条【新】记录,而那条异常还挂在
            原设备上。带上 模板/点位/编号 去拍,新记录才落得回同一台。 */}
        {needsFollowup && (
          <button
            className="ad-action is-primary"
            onClick={() => {
              setRetakeTarget({
                mode: "recheck",
                templateId: asset.templateId || "",
                pointId: asset.pointId || "",
                assetNo: asset.assetKey || asset.assetName || "",
                assetName: asset.assetName || "设备",
              });
              Toast.show({
                content: `复检:对准「${asset.assetName}」重新拍照即可`,
              });
              nav("/");
            }}
          >
            重新拍照复检
          </button>
        )}


        {/* 申请修改:旧版有、新版一直缺的发起端。审批闭环原来是断的 ——
            主管能批能驳,巡检员却发不出申请,发现填错了只能找人口头说。 */}
        <button className="ad-action" onClick={() => setCrOpen(true)}>
          申请修改
        </button>

        {/* 相关申请。只在真有申请时出现 —— 常年挂一个"暂无申请"的空标题是噪音。
            标题按角色变:一线人员这里只拿得到自己发起的,写"相关申请"是撒谎。 */}
        {reqs && reqs.length > 0 && (
          <>
            <div className="ad-title">
              {isMgmt ? "相关申请" : "我的申请"} · {reqs.length} 条
            </div>
            <div className="ad-reqs">
              {reqs.map((c) => (
                <div className="req-item" key={c.id}>
                  <div className="req-head">
                    <span className={"req-status is-" + c.status}>
                      {CR_STATUS[c.status] || c.status}
                    </span>
                    {/* 谁提的。管理角色看到的是别人的申请,
                        不写名字就不知道该找谁核实。 */}
                    {c.requestedBy && (
                      <span className="req-who">{c.requestedBy}</span>
                    )}
                    <span className="req-when">{fmtWhen(c.requestedAt)}</span>
                  </div>
                  {c.reason && <div className="req-reason">{c.reason}</div>}
                </div>
              ))}
            </div>
          </>
        )}


        {/* 巡检历史 */}
        <div className="ad-title">巡检历史</div>
        {snaps.length === 0 ? (
          <div className="task-empty">暂无历史记录</div>
        ) : (
          <div className="ad-history">
            {/* 【每条默认折叠,只第一条展开】—— 旧版的做法。
                AI 总结是一整段一两百字的话,全部铺开的话十条历史就是一屏又一屏
                的文字墙,想找"上次是什么时候、什么状态"反而要一直滑。
                折叠后一眼能看到的是时间 / 巡检人 / 状态,这三样才是扫的时候要的。

                用原生 <details>:展开收起不需要状态管理,而且长按选中、
                无障碍朗读、Ctrl+F 页内查找都能正常工作(自己写的展开做不到)。 */}
            {shownSnaps.map((snap, i) => {
              const rec = recCache[snap.recordId];
              const busy = recBusy[snap.recordId];
              const photos = (rec?.images || []).map((img) => ({
                url: `/storage/uploads/${rec!.id}/${img.id}_${img.fileName}`,
                key: img.id,
              }));
              // 只列有值的字段 —— 空字段铺一屏"—"没有任何信息量
              const filled = (rec?.fields || []).filter(
                (f) => String(f.value ?? "").trim() !== "",
              );
              return (
                <details
                  className="hist-item"
                  key={snap.id}
                  open={i === 0}
                  onToggle={(e) => {
                    if (e.currentTarget.open) void loadRecord(snap.recordId);
                  }}
                >
                  <summary className="hist-head">
                    <span className="hist-when">{fmtWhen(snap.createdAt)}</span>
                    {snap.inspector && (
                      <span className="hist-who">{snap.inspector}</span>
                    )}
                    <StatusTag text={snap.status || "—"} />
                  </summary>

                  {/* 照片。放在最前面 —— 巡检的证据是照片,文字是对照片的转述。 */}
                  {photos.length > 0 && (
                    <div className="hist-photos">
                      {photos.map((ph) => (
                        <span className="hist-thumb" key={ph.key}>
                          <Image src={ph.url} radius={8} />
                        </span>
                      ))}
                    </div>
                  )}

                  {/* 原始字段:AI 总结是转述,这里是当时逐项填的值。
                      两者都要 —— 总结读得快,原始值才是能追责的那份。 */}
                  {filled.length > 0 && (
                    <div className="hist-kv">
                      {filled.map((f) => {
                        const bad = fieldIsBad(f);
                        return (
                          <div
                            className={"hist-kv-row" + (bad ? " is-bad" : "")}
                            key={f.code}
                          >
                            <span className="hist-kv-k">{f.label}</span>
                            <span className="hist-kv-v">{String(f.value)}</span>
                          </div>
                        );
                      })}
                    </div>
                  )}

                  {busy && !rec && (
                    <div className="hist-body is-empty">正在取这次的照片和字段…</div>
                  )}

                  {snap.summary ? (
                    <div className="hist-body">{snap.summary}</div>
                  ) : (
                    <div className="hist-body is-empty">这次巡检没有留下总结</div>
                  )}
                </details>
              );
            })}

            {/* 超过 5 条折叠成一个按钮。展开后仍受分页限制 ——
                下面那个「加载更多」管的是"从服务器再取一页",这个管的是
                "本地已经取到的先只显示前 5 条",两件事不一样,不能合并。 */}
            {snaps.length > HISTORY_LIMIT && (
              <button
                className="hist-more"
                onClick={() => setHistExpanded((v) => !v)}
              >
                {histExpanded
                  ? "收起"
                  : `查看更多 ${snaps.length - HISTORY_LIMIT} 次`}
              </button>
            )}
          </div>
        )}

        {page < totalPages && (
          <button className="lg-clear" onClick={() => void loadMore()}>
            加载更多({page}/{totalPages})
          </button>
        )}
      </div>

      {asset && (
        <ChangeRequestSheet
          visible={crOpen}
          onClose={() => setCrOpen(false)}
          asset={asset}
          history={snaps}
          onSubmitted={() => nav("/approvals")}
        />
      )}
    </div>
  );
}
