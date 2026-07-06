import { Card } from "antd";

// 未迁移页面的占位:标明该页仍以旧版为准,避免误以为功能缺失
export default function Placeholder({ title }: { title: string }) {
  return (
    <Card>
      <h2 style={{ marginTop: 0 }}>{title}</h2>
      <p style={{ color: "#5b6b78", margin: 0 }}>
        该页面尚未迁移到新版,当前请使用旧版后台(admin-frontend)操作;迁移完成后此处替换为正式页面。
      </p>
    </Card>
  );
}
