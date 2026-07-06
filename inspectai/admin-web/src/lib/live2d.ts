// Live2D 看板娘(oh-my-live2d)单例:全局只初始化一次,由 Agent 页控制显隐。
// 模型从 oml2d 官方模型 CDN 拉取;离线/被墙时静默降级,不影响页面。
import type { Oml2dEvents, Oml2dMethods, Oml2dProperties } from "oh-my-live2d";

type Oml2d = Oml2dProperties & Oml2dMethods & Oml2dEvents;

let instance: Oml2d | null = null;
let loading = false;

export async function ensureLive2d(): Promise<Oml2d | null> {
  if (instance) return instance;
  if (loading) return null;
  loading = true;
  try {
    const { loadOml2d } = await import("oh-my-live2d");
    instance = loadOml2d({
      dockedPosition: "right", // 右下角,不遮左侧导航
      sayHello: false,
      menus: { disable: true },
      statusBar: { disable: true },
      transitionTime: 600,
      models: [
        {
          // 模型自托管在 public/live2d(相对路径,dev 与 /v2/ 生产都能解析),不依赖外网 CDN
          path: "live2d/shizuku/shizuku.model.json",
          scale: 0.14,
          position: [-20, 20],
          stageStyle: { height: 240, width: 190, marginBottom: "96px" },
        },
      ],
      tips: {
        idleTips: {
          interval: 30000,
          message: ["我是智巡 Agent 的小助手", "有异常记得及时复检哦", "点右上角可以看历史对话"],
        },
      },
    }) as unknown as Oml2d;
    return instance;
  } catch {
    return null; // CDN 不可达时降级:无看板娘,功能不受影响
  } finally {
    loading = false;
  }
}

export function live2dSay(text: string, duration = 4000, priority = 4) {
  instance?.tipsMessage(text, duration, priority);
}

export function live2dShow() {
  void instance?.stageSlideIn();
}

export function live2dHide() {
  void instance?.stageSlideOut();
}
