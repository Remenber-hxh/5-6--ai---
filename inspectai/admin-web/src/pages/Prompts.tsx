import { Button, Space, Typography } from "antd";
import { useState } from "react";
import { useSearchParams } from "react-router-dom";

import PromptDraft from "../components/PromptDraft";
import PromptVersions from "../components/PromptVersions";
import TemplateWorkbench from "../components/TemplateWorkbench";

// ===== 巡检模板 =====
//
// 【这一页原来是三个页签】「提示词」「巡检模板」「提交规则」,各编同一份模板
// 的一个侧面。配一个「机房卫生」要走三处:叫什么在模板页、必不必填在提交
// 规则页、AI 怎么判在提示词页 —— 人心里是一件事,界面上要走三处。
//
// 那个拆分不是设计出来的,是接口形状逼出来的:三个写入口各写字段表的一部分,
// 互相错开以免冲掉。代价是还得配一套跨页同步("一页存盘另外两页要重读"、
// "三页共用一个当前模板"),而那套机制本身就是拆错了的证据 ——
// 它解决的问题,是拆分自己造出来的。
//
// 后端合并之后(一个 PUT 写完整份,见 report_template_admin.go),
// 这里跟着合成一个工作台,那套同步机制整个删掉。

const { Title, Text } = Typography;

export default function Prompts() {
  // 【模板 id 留在地址栏】刷新还在同一份模板上,链接也发得出去。
  // 用 replace 不用 push:切模板不该往浏览器历史里堆。
  const [params, setParams] = useSearchParams();
  const tplId = params.get("tpl") || "";
  const setTplId = (id: string) => {
    const next = new URLSearchParams(params);
    if (id) next.set("tpl", id);
    else next.delete("tpl");
    setParams(next, { replace: true });
  };

  const [showVersions, setShowVersions] = useState(false);
  // 改完存盘后让工作台重新拉一次 —— 回滚历史版本之后手里那份就过期了
  const [reloadKey, setReloadKey] = useState(0);

  return (
    <Space direction="vertical" size={16} style={{ width: "100%" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
        <Title level={4} style={{ margin: 0 }}>
          巡检模板
        </Title>
        <Text type="secondary" style={{ fontSize: 13 }}>
          改完保存,现场下一次打开就是新的,不用发版
        </Text>
        <div style={{ marginLeft: "auto" }}>
          <Space>
            <PromptDraft onCreated={(id) => setTplId(id)} />
            <Button type="text" disabled={!tplId} onClick={() => setShowVersions(true)}>
              历史
            </Button>
          </Space>
        </div>
      </div>

      <TemplateWorkbench
        key={reloadKey}
        templateId={tplId}
        onTemplateChange={setTplId}
      />

      {tplId && (
        <PromptVersions
          templateId={tplId}
          open={showVersions}
          onClose={() => setShowVersions(false)}
          onRestored={() => {
            setShowVersions(false);
            // 回滚之后工作台手里那份是回滚前的,拿它去存会把刚回滚的盖掉
            setReloadKey((n) => n + 1);
          }}
        />
      )}
    </Space>
  );
}
