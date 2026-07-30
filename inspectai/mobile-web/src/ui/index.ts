// ===== 组件出口(防腐层) =====
//
// 业务代码一律从这里引组件,不直接依赖任何组件库。
// 好处:换库时只改这一层,13 个页面一行都不用动;
// 各库之间的 API 差异也集中在这里,不会散落各处漏改。
//
// 当前底层:@arco-design/mobile-react(按需引入,含各组件独立样式)

export { Toast } from "./toast";
export { Dialog } from "./dialog";
export { Button } from "./Button";
export { Input } from "./Input";
