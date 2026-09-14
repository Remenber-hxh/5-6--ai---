import { DeleteOutlined, DownOutlined, HolderOutlined, RightOutlined } from "@ant-design/icons";
import { Input, Select, Space, Switch, Tag, Tooltip } from "antd";

import { TemplateFieldDTO } from "../api/mgmt";

// ===== 一个检查项 = 一张卡 =====
//
// 【为什么要有这个组件】原来配一个「机房卫生」要走三个页签:
// 叫什么在「巡检模板」、必不必填在「提交规则」、AI 怎么判在「提示词」。
// 人心里是一件事,界面上要走三处 —— 而且三个页签各存各的,
// 还得靠一套跨页同步机制兜着"一页存盘另外两页要重读"。
//
// 收成一张卡之后,那套同步机制整个不需要了:一处配置、一次存盘。

/** 判定模式 → 现场能看懂的说法。系统那套 read_text / visual_lenient 不给人看。 */
export const MODE_LABELS: Record<string, { title: string; hint: string }> = {
  "": { title: "AI 不填,我自己填", hint: "这一项留给现场手填" },
  read_text: { title: "读屏上的数字", hint: "表盘、LCD、字轮 —— 看不清就不填" },
  visual: { title: "看图判断", hint: "看得出来才判,拿不准就不填" },
  visual_lenient: { title: "看图判断(宽松)", hint: "拍到就判;只有明显异常才判不合格" },
  functional_test: { title: "看现场测试动作", hint: "比如防夹测试,拍到动作就算通过" },
  objective_date: { title: "查日期", hint: "和当天日期比,判过没过期" },
  sensory: { title: "靠听/闻 —— 留人工", hint: "照片感知不了,只在看得见异常时才判" },
  system: { title: "系统自动填", hint: "日期、巡检人这类,不用 AI 也不用人填" },
  summary: { title: "汇总不合格项", hint: "别的项判了不合格,在这里逐条写明" },
};

/** 判定模式要不要展开「什么算合格 / 不合格 / 别填」那三行 */
function needsCriteria(mode: string): boolean {
  return mode !== "" && mode !== "system" && mode !== "summary";
}

export interface FieldCardProps {
  field: TemplateFieldDTO;
  expanded: boolean;
  /** 有巡检记录之后字段标识不能再改 —— 改了历史记录里那一项就查不出来 */
  codeLocked: boolean;
  onToggle: () => void;
  onPatch: (patch: Partial<TemplateFieldDTO>) => void;
  onRemove: () => void;
}

export default function FieldCard({
  field,
  expanded,
  codeLocked,
  onToggle,
  onPatch,
  onRemove,
}: FieldCardProps) {
  const mode = field.judgeMode || "";
  const modeInfo = MODE_LABELS[mode] || MODE_LABELS[""];

  return (
    <div
      style={{
        border: "1px solid #eee",
        borderRadius: 8,
        marginBottom: 8,
        background: expanded ? "#fafafa" : "#fff",
      }}
    >
      {/* 收起时这一行要能回答"这一项是什么、AI 管不管" —— 不用点开 */}
      <div
        onClick={onToggle}
        style={{
          display: "flex",
          alignItems: "center",
          gap: 10,
          padding: "10px 12px",
          cursor: "pointer",
        }}
      >
        {expanded ? <DownOutlined style={{ fontSize: 11, color: "#999" }} /> : <RightOutlined style={{ fontSize: 11, color: "#999" }} />}
        <HolderOutlined style={{ color: "#ccc" }} />
        <span style={{ flex: 1, minWidth: 0 }}>
          {field.label || <span style={{ color: "#bbb" }}>未命名检查项</span>}
          {field.required && <span style={{ color: "#ff4d4f", marginLeft: 4 }}>*</span>}
        </span>
        <Tag color={mode ? "blue" : undefined} style={{ marginRight: 0 }}>
          {modeInfo.title}
        </Tag>
        <Tooltip title="删掉这一项">
          <DeleteOutlined
            onClick={(e) => {
              e.stopPropagation();
              onRemove();
            }}
            style={{ color: "#bbb" }}
          />
        </Tooltip>
      </div>

      {expanded && (
        <div style={{ padding: "0 12px 14px 34px", display: "grid", gap: 12 }}>
          <Space size={10} wrap>
            <Field label="这一项叫什么">
              <Input
                value={field.label}
                placeholder="机房卫生"
                style={{ width: 200 }}
                onChange={(e) => onPatch({ label: e.target.value })}
              />
            </Field>
            <Field label="现场填什么">
              <Select
                value={field.kind}
                style={{ width: 140 }}
                onChange={(v) =>
                  onPatch({
                    kind: v,
                    // 【切成单选要给默认选项】只有一个选项的单选存不进去
                    // (后端会拦),而人切过来看见空选项区不会想到要去填。
                    options: v === "choice" && !field.options?.length ? ["正常", "异常"] : field.options,
                  })
                }
                options={[
                  { value: "text", label: "文字" },
                  { value: "number", label: "数字" },
                  { value: "choice", label: "选一个" },
                ]}
              />
            </Field>
            <Field label="现场必须填">
              <Switch checked={field.required} onChange={(v) => onPatch({ required: v })} />
            </Field>
          </Space>

          {field.kind === "choice" && (
            <Field label="可选项(逗号隔开,至少两个)">
              <Input
                value={(field.options || []).join("，")}
                placeholder="正常，异常"
                onChange={(e) =>
                  onPatch({
                    options: e.target.value
                      .split(/[，,]/)
                      .map((s) => s.trim())
                      .filter(Boolean),
                  })
                }
              />
            </Field>
          )}

          <Field label="AI 怎么帮我填">
            <Select
              value={mode}
              style={{ width: 260 }}
              onChange={(v) => onPatch({ judgeMode: v })}
              options={Object.entries(MODE_LABELS).map(([value, m]) => ({
                value,
                label: m.title,
              }))}
            />
            <div style={{ fontSize: 12, color: "#999", marginTop: 4 }}>{modeInfo.hint}</div>
          </Field>

          {needsCriteria(mode) && (
            <div style={{ display: "grid", gap: 8 }}>
              {/* 【用"什么样算…"而不是 yes_when / no_when】后者是我写给自己看的。
                  现场主管看不懂 yes_when,但看得懂"什么样算合格"。 */}
              <Criteria
                color="#389e0d"
                label={field.kind === "number" ? "什么情况下读数才算数" : "什么样算合格"}
                value={field.yesWhen || ""}
                placeholder="地面基本整洁,无明显杂物堆放"
                onChange={(v) => onPatch({ yesWhen: v })}
              />
              {field.kind !== "number" && (
                <Criteria
                  color="#cf1322"
                  label="什么样算不合格"
                  value={field.noWhen || ""}
                  placeholder="明显堆放杂物、积水漏油"
                  onChange={(v) => onPatch({ noWhen: v })}
                />
              )}
              <Criteria
                color="#8c8c8c"
                label="什么情况别填"
                value={field.skipWhen || ""}
                placeholder="没拍到机房地面"
                onChange={(v) => onPatch({ skipWhen: v })}
              />
              <Criteria
                color="#8c8c8c"
                label="额外提醒(可留空)"
                value={field.judgeNote || ""}
                placeholder="少量灰尘、脚印不算异常"
                onChange={(v) => onPatch({ judgeNote: v })}
              />
            </div>
          )}

          <Field label={codeLocked ? "字段标识(已有记录,不能改)" : "字段标识"}>
            <Input
              value={field.code}
              disabled={codeLocked}
              placeholder="room_clean"
              style={{ width: 200, fontFamily: "monospace" }}
              onChange={(e) => onPatch({ code: e.target.value })}
            />
            <div style={{ fontSize: 12, color: "#999", marginTop: 4 }}>
              存数据用的英文名。有记录之后不能再改 —— 改了历史记录里这一项就查不出来
            </div>
          </Field>
        </div>
      )}
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div style={{ fontSize: 12, color: "#888", marginBottom: 4 }}>{label}</div>
      {children}
    </div>
  );
}

function Criteria({
  color,
  label,
  value,
  placeholder,
  onChange,
}: {
  color: string;
  label: string;
  value: string;
  placeholder: string;
  onChange: (v: string) => void;
}) {
  return (
    <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
      <span style={{ fontSize: 12, color, minWidth: 132, textAlign: "right" }}>{label}</span>
      <Input value={value} placeholder={placeholder} onChange={(e) => onChange(e.target.value)} />
    </div>
  );
}
