// ===== 整屏加载 =====
//
// 4 个页面 5 处各写了一遍 `<div className="center-screen"><span className="spinner"/></div>`。
//
// 注意和 LoadingScene 的分工:
//   CenterLoading  页面数据还没到,几百毫秒的事,不需要解释
//   LoadingScene   AI 识别那种十几秒的等待,要告诉用户"正在逐张查看照片"，
//                  否则那十几秒会被当成卡死
// 别用错:短等待配长文案显得啰嗦,长等待配一个转圈会让人以为死了。

export default function CenterLoading({ text }: { text?: string }) {
  return (
    <div className="center-screen">
      <span className="spinner" />
      {text && <p className="loading-sub">{text}</p>}
    </div>
  );
}
