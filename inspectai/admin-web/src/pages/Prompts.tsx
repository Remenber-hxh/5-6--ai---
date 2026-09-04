import {
  Button,
  Card,
  Col,
  Input,
  Modal,
  Row,
  Segmented,
  Select,
  Skeleton,
  Space,
  Table,
  Tabs,
  Tag,
  message,
} from "antd";
import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";

import PromptDraft from "../components/PromptDraft";
import PromptVersions from "../components/PromptVersions";
import TemplateEditor from "../components/TemplateEditor";
import TemplateFieldRules from "../components/TemplateFieldRules";
import {
  PromptField,
  PromptMode,
  PromptTemplate,
  PromptTemplateRow,
  builtinPrompt,
  getPromptTemplate,
  listPromptTemplates,
  renderPromptTemplate,
  savePromptTemplate,
} from "../api/mgmt";
import { C } from "../styles/tokens";

// 提示词模板中心:字段表可编辑(业务人员直接改 AI 判定规则),保存即时生效,可预览渲染后的完整 Prompt
export default function Prompts() {
  const [templates, setTemplates] = useState<PromptTemplateRow[]>([]);
  const [modes, setModes] = useState<{ value: string; label: string }[]>([]);
  const [current, setCurrent] = useState<PromptTemplate | null>(null);
  const [preview, setPreview] = useState("");
  const [saving, setSaving] = useState(false);
  const [loading, setLoading] = useState(true);
  const [showVersions, setShowVersions] = useState(false);
  // 内置底稿:载不到就是这个模板压根没有内置提示词(AI 不识别)
  const [builtin, setBuiltin] = useState<{ text: string; found: boolean } | null>(null);
  const [loadingBuiltin, setLoadingBuiltin] = useState(false);

  // 【Tab 状态写进地址栏】不写的话:刷新回到第一个 Tab、想把「提交规则」
  // 发给同事只能说"你点进去往右切一下"。
  //
  // 用 replace 不用 push:切 Tab 不该往浏览器历史里堆 —— 否则连切五下,
  // 想退出这个页面要按五次返回。刷新保持和链接可分享这两个好处不受影响。
  const [params, setParams] = useSearchParams();
  const raw = params.get("tab") || "";
  const tab = raw === "rules" || raw === "template" ? raw : "prompt";
  const setTab = (k: string) => {
    const next = new URLSearchParams(params);
    if (k === "prompt") next.delete("tab");
    else next.set("tab", k);
    setParams(next, { replace: true });
  };

  // 【三个页签共用一个"当前模板"】原来每一页各有一个选择器,各选各的:
  // 在提示词页选好「电梯巡检(有机房)」,切到巡检模板页是另一个模板,
  // 切到提交规则页又是第三个 —— 人以为自己一直在配同一份,
  // 实际上改的是三份不同的东西,而且界面上没有任何地方提示。
  // 把它提到地址栏上,三页读同一个值。
  const tplId = params.get("tpl") || "";
  // 【一页存盘,另外两页要重读】三个页签编的是同一份模板的不同侧面,
  // 但保存写的都是整份。已经打开的页签手里是保存前那一版 —— 拿它去存,
  // 会把别的页刚存进去的东西按旧值写回来,而且两边都提示"已保存"。
  // 存盘成功后把这个数一加,另外两页重新拉一次(改了一半的不动)。
  //
  // 【每页一个计数,自己存盘不触发自己】共用一个计数的话,存完盘的那一页
  // 会被自己的计数触发,刚拉过的数据再拉一遍。
  const [savedByPrompt, setSavedByPrompt] = useState(0);
  const [savedByEditor, setSavedByEditor] = useState(0);
  const [savedByRules, setSavedByRules] = useState(0);
  const setTplId = (id: string) => {
    const next = new URLSearchParams(params);
    if (id) next.set("tpl", id);
    else next.delete("tpl");
    setParams(next, { replace: true });
  };
  const [dirty, setDirty] = useState(false);

  useEffect(() => {
    listPromptTemplates()
      .then((d) => {
        setTemplates(d.templates || []);
        setModes(d.modes || []);
        // 地址栏带了模板就用它(从别的页签切过来的情况);带的是个
        // 这一页没有的 id 就退回第一个,而不是留一张空白页。
        const list = d.templates || [];
        const want = list.find((t) => t.id === tplId) || list[0];
        if (want) void select(want.id);
      })
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function select(id: string) {
    const t = await getPromptTemplate(id);
    if (id !== tplId) setTplId(id);
    setCurrent(t);
    setDirty(false);
    setBuiltin(null);
    // 没自定义过的模板,先去问一下"现在实际用的是哪一段" ——
    // 【不问的话人只能对着空白框猜】而他要改的正是那一段。
    if (t.mode === "raw" && !(t.rawText || "").trim()) {
      setLoadingBuiltin(true);
      builtinPrompt(id)
        .then((d) => setBuiltin({ text: d.prompt || "", found: !!d.found }))
        .catch(() => setBuiltin({ text: "", found: false }))
        .finally(() => setLoadingBuiltin(false));
    }
  }

  // AI 新建的模板:入库已经完成了,这里只负责让界面跟上 ——
  // 【不跟上的话人会以为没建成】列表里没有、下拉里也选不中,
  // 他会再点一次生成,于是库里多出第二份一模一样的模板。
  async function adoptNewTemplate(id: string) {
    const d = await listPromptTemplates().catch(() => null);
    if (d) setTemplates(d.templates || []);
    setSavedByPrompt((n) => n + 1); // 另外两页也要重拉列表,否则看不到新模板
    switchTemplate(id);
  }

  // 切换模板前拦未保存改动(判定规则改一半丢了会直接影响识别)
  function switchTemplate(id: string) {
    if (!dirty) {
      setTplId(id);
      return void select(id);
    }
    // 【确认之前不写地址栏】写了的话,人点"留在本页"之后本页还是旧模板,
    // 而另外两个页签已经按地址栏跳到新模板去了 —— 三页又对不上了。
    Modal.confirm({
      title: "当前模板有未保存的修改",
      content: "切换后未保存的判定规则修改会丢失,确认切换?",
      okText: "放弃修改并切换",
      cancelText: "留在本页",
      onOk: () => {
        setTplId(id);
        void select(id);
      },
    });
  }

  function patch(next: PromptTemplate) {
    setCurrent(next);
    setDirty(true);
  }

  function patchField(idx: number, key: keyof PromptField, value: string) {
    if (!current) return;
    patch({ ...current, fields: current.fields.map((f, i) => (i === idx ? { ...f, [key]: value } : f)) });
  }

  // 版本备注自动生成。
  //
  // 【不弹框问人】保存是最常做的动作,每次多一步输入,人很快就会开始填
  // "改了一下"、"1"、"." —— 拿到的不是信息,是噪音。而"字段表 24 项"/
  // "整段文本 1820 字"这种事实,恰恰能一眼看出哪一版是大改哪一版是微调。
  function autoNote(t: PromptTemplate): string {
    return t.mode === "raw"
      ? `整段文本 · ${(t.rawText || "").trim().length} 字`
      : `字段表 · ${t.fields?.length || 0} 项`;
  }

  async function save() {
    if (!current) return;
    setSaving(true);
    try {
      await savePromptTemplate(current, autoNote(current));
      setDirty(false);
      setBuiltin(null);
      setSavedByPrompt((n) => n + 1);
      message.success("已保存,下一次识别开始生效");
      // 列表上的"已自定义"标记要跟着变,否则界面还说它在用内置
      listPromptTemplates()
        .then((d) => setTemplates(d.templates || []))
        .catch(() => undefined);
    } catch (e) {
      message.error(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }

  // 切换维护方式。
  //
  // 【字段表 → 整段文本要带上渲染结果】切过去给一个空白框的话,
  // 人得从零重写一份已经调好的提示词 —— 没人会这么做,于是这个出口废掉。
  async function switchMode(mode: PromptMode) {
    if (!current || mode === (current.mode || "structured")) return;
    if (mode === "raw") {
      let seed = (current.rawText || "").trim();
      if (!seed) {
        try {
          seed = await renderPromptTemplate(current.id);
        } catch {
          seed = builtin?.text || "";
        }
      }
      patch({ ...current, mode, rawText: seed });
      return;
    }
    // 反方向是不可逆的:整段正文没法拆回字段表。说清楚再让人决定。
    Modal.confirm({
      title: "改用字段表维护?",
      content: "已写好的整段正文不会被自动拆成字段,切回去后需要逐项填写。正文本身会保留在历史版本里。",
      okText: "改用字段表",
      cancelText: "取消",
      onOk: () => patch({ ...current, mode }),
    });
  }

  function useBuiltinAsDraft() {
    if (!current || !builtin?.text) return;
    patch({ ...current, mode: "raw", rawText: builtin.text });
  }

  async function doPreview() {
    if (!current) return;
    setPreview(await renderPromptTemplate(current.id));
  }

  const isRaw = (current?.mode || "structured") === "raw";
  const rawEmpty = isRaw && !(current?.rawText || "").trim();

  // 整段文本编辑器。
  //
  // 【空正文不是"没写"—— 是"用内置那份"】所以空的时候不能只摆一个空白框:
  // 人看不出现在到底在用什么,也不知道要不要写。这里分三种情况说清楚,
  // 每一种都给出下一步能点的东西。
  const rawPane = current && (
    <>
      {rawEmpty && (
        <div
          style={{
            border: `1px solid ${C.line}`,
            borderRadius: 6,
            padding: "10px 12px",
            marginBottom: 10,
            display: "flex",
            alignItems: "center",
            gap: 10,
            flexWrap: "wrap",
          }}
        >
          {loadingBuiltin ? (
            <span style={{ fontSize: 13, color: C.textFaint }}>正在读取当前生效的提示词…</span>
          ) : builtin?.found ? (
            <>
              <span style={{ fontSize: 13, color: C.textSub }}>
                当前用的是系统内置提示词,还没有在后台改过。
              </span>
              <Button size="small" onClick={useBuiltinAsDraft}>
                载入内置内容开始编辑
              </Button>
            </>
          ) : (
            // 【这三个模板的真相】消防泵房 / UPS 机房 / 生活水泵房 从来没有
            // 提示词,AI 在它们上面一次都没跑过 —— 现场一直是纯人工填。
            // 说出来,才有人知道写一段就能把它们打开。
            <span style={{ fontSize: 13, color: C.textSub }}>
              这个模板还没有提示词,AI 不会识别它的照片,现场只能人工填写。
              在下面写一段并保存,即可启用。
            </span>
          )}
        </div>
      )}
      <Input.TextArea
        value={current.rawText || ""}
        onChange={(e) => patch({ ...current, rawText: e.target.value })}
        autoSize={{ minRows: 18, maxRows: 40 }}
        placeholder="直接写提示词正文。写什么,模型就收到什么。"
        style={{ fontFamily: "ui-monospace, Menlo, Consolas, monospace", fontSize: 12.5, lineHeight: 1.7 }}
      />
      <div style={{ marginTop: 8, display: "flex", alignItems: "center", gap: 12 }}>
        <span style={{ fontSize: 12, color: C.textFaint, fontVariantNumeric: "tabular-nums" }}>
          {(current.rawText || "").length} 字
        </span>
        {!rawEmpty && (
          // 清空 = 回到内置那份。【这是唯一一条"退回出厂"的路】,
          // 而它藏在"把框里的字删光"这个动作里,不说没人找得到。
          <Button
            type="text"
            size="small"
            onClick={() =>
              Modal.confirm({
                title: "恢复为内置提示词?",
                content: "清空后,这个模板会重新使用系统内置的那份。当前内容会保留在历史版本里。",
                okText: "恢复内置",
                cancelText: "取消",
                onOk: () => patch({ ...current, rawText: "" }),
              })
            }
          >
            恢复为内置
          </Button>
        )}
      </div>
    </>
  );

  // 提示词那一屏的内容(加载中先摆骨架,否则 Tab 会闪一下不见)
  const promptPane =
    loading && !current ? (
      <Card title="提示词模板">
        <Skeleton active paragraph={{ rows: 8 }} />
      </Card>
    ) : (
    <>
    <Card
      size="small"
      title={
        <Space>
          提示词模板
          {dirty && <Tag color="orange">未保存</Tag>}
        </Space>
      }
      extra={
        <Space>
          <Select
            style={{ width: 240 }}
            value={current?.id}
            options={templates.map((t) => ({ value: t.id, label: t.name || t.id }))}
            onChange={switchTemplate}
          />
          {/* 【入口在这一排】它建的是一份新模板,和旁边的模板下拉是一件事;
              放在下面字段表上方的话,看上去像在对当前这份模板做什么。 */}
          <PromptDraft onCreated={adoptNewTemplate} />
          <Button type="text" onClick={() => setShowVersions(true)}>
            历史
          </Button>
          <Button type="text" onClick={doPreview}>
            预览
          </Button>
          <Button type="primary" loading={saving} disabled={!dirty} onClick={save}>
            保存
          </Button>
        </Space>
      }
    >
      {current && (
        <>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 12,
              marginBottom: 14,
            }}
          >
            <Segmented
              size="small"
              value={current.mode || "structured"}
              options={[
                // 【没有字段表的模板不给切过去】切过去只会得到一张空表,
                // 而这张表还不能加行(字段编辑器另做)—— 人会卡在一个
                // 既填不了、也存不了的界面上。
                { label: "字段表", value: "structured", disabled: !current.fields?.length },
                { label: "整段文本", value: "raw" },
              ]}
              onChange={(v) => void switchMode(v as PromptMode)}
            />
            <span style={{ fontSize: 12.5, color: C.textFaint }}>
              {(current.mode || "structured") === "structured"
                ? "逐项填判定依据,系统自动组装成提示词"
                : current.fields?.length
                  ? "直接写整段提示词,写什么模型就收到什么"
                  : "直接写整段提示词。这个模板还没有字段表"}
            </span>
          </div>
          {isRaw ? (
            rawPane
          ) : (
          <>
          <Row gutter={12} style={{ marginBottom: 12 }}>
            {/* 【两栏等宽等高】原来是 14/6:左边一个单行框、右边一个带滚动条的
                三行框,右侧还空出 4 栏。两个同级的东西长得完全不一样,
                看上去像右边那个出了问题。 */}
            <Col span={12}>
              <div style={lbl}>场景描述</div>
              <Input.TextArea
                autoSize={{ minRows: 2, maxRows: 5 }}
                value={current.scene}
                onChange={(e) => patch({ ...current, scene: e.target.value })}
                placeholder="这个场景长什么样、拍的时候站在哪"
              />
            </Col>
            <Col span={12}>
              <div style={lbl}>必拍照片要求(一行一条)</div>
              {/* 后端是字符串数组,一行一条。以前这里当成单个字符串,
                  一旦编辑就提交出一个类型对不上的值,保存直接失败。 */}
              <Input.TextArea
                autoSize={{ minRows: 1, maxRows: 4 }}
                value={(current.expectedPhotos || []).join("\n")}
                onChange={(e) =>
                  patch({
                    ...current,
                    expectedPhotos: e.target.value.split("\n").filter((s) => s.trim() !== ""),
                  })
                }
              />
            </Col>
          </Row>
          <Table<PromptField>
            rowKey="code"
            size="small"
            dataSource={current.fields}
            pagination={false}
            scroll={{ x: 1210 }}
            /* 一个模板有十几到二十几个字段。表头滚没了之后,中间那几列
               四个输入框长得一模一样,人分不清哪一列是"判否"哪一列是
               "不返回" —— 填错列 AI 就按反的规则判。 */
            sticky={{ offsetHeader: 0 }}
            columns={[
              {
                title: "字段",
                dataIndex: "code",
                width: 170,
                fixed: "left",
                render: (v, f) => (
                  <div>
                    <div style={{ fontWeight: 600 }}>{f.label}</div>
                    <div style={{ color: C.textFaint, fontSize: 12 }}>
                      {v}
                      {/* 分组本来就存在数据里(头部/机房/轿厢层站/汇总),
                          界面上不显示的话,二十几行看上去是一锅平的。 */}
                      {f.group && <span style={{ marginLeft: 6 }}>· {f.group}</span>}
                    </div>
                  </div>
                ),
              },
              {
                title: "判定模式",
                width: 200,
                render: (_, f, i) => (
                  <Select
                    size="small"
                    /* 【实测原来 124px 每一项都被截断】"系统注入(不识别)"
                       显示成"系统注入(不识…" —— 而这一列决定这个字段
                       到底走不走 AI,看不全就等于没标。最长的一项是
                       "读取文本(清晰才返回)",按它定宽。 */
                    style={{ width: 188 }}
                    value={f.mode}
                    options={modes}
                    onChange={(v) => patchField(i, "mode", v)}
                  />
                ),
              },
              {
                title: "判「是」看什么",
                render: (_, f, i) => (
                  <Input.TextArea
                    size="small"
                    /* 【封顶 6 行】不封顶的话,一条写长了的依据能把整行撑到
                       十几行高,同一屏就只剩两个字段。 */
                    autoSize={{ minRows: 1, maxRows: 6 }}
                    placeholder="符合要求时照片上是什么样"
                    value={f.yesWhen}
                    onChange={(e) => patchField(i, "yesWhen", e.target.value)}
                  />
                ),
              },
              {
                title: "判「否」看什么",
                render: (_, f, i) => (
                  <Input.TextArea
                    size="small"
                    /* 【封顶 6 行】不封顶的话,一条写长了的依据能把整行撑到
                       十几行高,同一屏就只剩两个字段。 */
                    autoSize={{ minRows: 1, maxRows: 6 }}
                    placeholder="不符合时是什么样"
                    value={f.noWhen}
                    onChange={(e) => patchField(i, "noWhen", e.target.value)}
                  />
                ),
              },
              {
                title: "何时不返回(留人工)",
                render: (_, f, i) => (
                  <Input.TextArea
                    size="small"
                    /* 【封顶 6 行】不封顶的话,一条写长了的依据能把整行撑到
                       十几行高,同一屏就只剩两个字段。 */
                    autoSize={{ minRows: 1, maxRows: 6 }}
                    placeholder="什么情况下不下结论、留给人工"
                    value={f.skipWhen}
                    onChange={(e) => patchField(i, "skipWhen", e.target.value)}
                  />
                ),
              },
              {
                title: "备注",
                width: 180,
                render: (_, f, i) => (
                  <Input.TextArea
                    size="small"
                    autoSize
                    value={f.note}
                    onChange={(e) => patchField(i, "note", e.target.value)}
                  />
                ),
              },
            ]}
          />
          </>
          )}
        </>
      )}
      <Modal
        title="渲染后的完整 Prompt"
        open={!!preview}
        width={760}
        footer={null}
        onCancel={() => setPreview("")}
      >
        <pre style={{ whiteSpace: "pre-wrap", fontSize: 12.5, maxHeight: "60vh", overflow: "auto" }}>{preview}</pre>
      </Modal>
    </Card>
    {current && (
      <PromptVersions
        templateId={current.id}
        open={showVersions}
        onClose={() => setShowVersions(false)}
        onRestored={() => void select(current.id)}
      />
    )}
    </>
  );

  // 【一个侧栏入口,页内分板块】提示词和提交规则是两件事:一个管 AI 怎么判断,
  // 一个管表单要交什么才算数。堆在同一页要一直往下滚;拆成两个菜单项又让
  // 侧栏更长。Tab 两头都不占。
  return (
    <Tabs
      activeKey={tab}
      onChange={setTab}
      items={[
        { key: "prompt", label: "提示词", children: promptPane },
        // 【模板排在提交规则前面】提交规则是给模板里的字段配必填,
        // 先有字段才谈得上配它 —— 顺序反了人会先打开一个空的配置页。
        {
          key: "template",
          label: "巡检模板",
          children: (
            <TemplateEditor
              templateId={tplId}
              onTemplateChange={setTplId}
              reloadKey={savedByPrompt + savedByRules}
              onSaved={() => {
                setSavedByEditor((n) => n + 1);
                // 提示词页改了一半就别动它 —— 未保存的判定规则比同步重要
                if (!dirty && tplId) void select(tplId);
              }}
            />
          ),
        },
        {
          key: "rules",
          label: "提交规则",
          children: (
            <TemplateFieldRules
              templateId={tplId}
              onTemplateChange={setTplId}
              reloadKey={savedByPrompt + savedByEditor}
              onSaved={() => setSavedByRules((n) => n + 1)}
            />
          ),
        },
      ]}
    />
  );
}


const lbl: React.CSSProperties = { fontSize: 12, color: "#8aa0b0", marginBottom: 4 };
