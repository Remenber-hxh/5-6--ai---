import ArcoDialog from "@arco-design/mobile-react/esm/dialog";
import "@arco-design/mobile-react/esm/dialog/style/css";
import type { ReactNode } from "react";

// ===== Dialog 适配层 =====
//
// 这是本次迁移最危险的一处差异:
//   antd-mobile:`const ok = await Dialog.confirm(...)` 返回 Promise<boolean>
//   Arco:       回调式,返回 { close, update } —— 不是 Promise
//
// 若业务代码直接 await Arco 的返回值,会立刻得到一个真值对象,于是
// 「删除照片」「提交日报」这类操作会【跳过确认直接执行】。
// 不报错、不崩溃,只是确认框形同虚设 —— 所以必须在这里包回 Promise。

export interface ConfirmConfig {
  content: ReactNode;
  title?: ReactNode;
  confirmText?: string;
  cancelText?: string;
}

/** @returns 用户是否确认。取消、点遮罩关闭均为 false */
function confirm(config: ConfirmConfig): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    let settled = false;
    const done = (v: boolean) => {
      // onOk/onCancel 之后 onClose 还会再触发一次,只认第一次
      if (settled) return;
      settled = true;
      resolve(v);
    };
    ArcoDialog.confirm({
      title: config.title,
      // Arco 的内容放 children(继承自 DialogProps),不是 content
      children: config.content,
      okText: config.confirmText ?? "确定",
      cancelText: config.cancelText ?? "取消",
      onOk: () => done(true),
      onCancel: () => done(false),
      // 必须有:点遮罩/返回键关闭时若不 resolve,await 会永久挂起、页面卡死
      onClose: () => done(false),
    });
  });
}

export const Dialog = { confirm };
