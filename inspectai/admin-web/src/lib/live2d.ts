// Live2D 看板娘(oh-my-live2d)单例:全局只初始化一次,由 Agent 页控制显隐。
// 模型从 oml2d 官方模型 CDN 拉取;离线/被墙时静默降级,不影响页面。
import type { Oml2dEvents, Oml2dMethods, Oml2dProperties } from "oh-my-live2d";

type Oml2d = Oml2dProperties & Oml2dMethods & Oml2dEvents;

let instance: Oml2d | null = null;
let container: HTMLDivElement | null = null;
let loadingPromise: Promise<Oml2d | null> | null = null;

export function ensureLive2d(): Promise<Oml2d | null> {
  if (instance) return Promise.resolve(instance);
  // 并发调用(如 StrictMode 双挂载)复用同一个加载 Promise,而不是返回 null 放弃
  if (loadingPromise) return loadingPromise;
  loadingPromise = doLoad();
  return loadingPromise;
}

async function doLoad(): Promise<Oml2d | null> {
  try {
    const { loadOml2d } = await import("oh-my-live2d");
    // 自有容器:显隐由我们控制(滑出动画不可靠,离开 Agent 页直接藏容器)
    container = document.createElement("div");
    container.style.display = "none";
    document.body.appendChild(container);
    instance = loadOml2d({
      parentElement: container,
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
    loadingPromise = null; // 失败允许下次重试;成功后保持单例
    return null; // 模型加载失败时降级:无看板娘,功能不受影响
  }
}

export function live2dSay(text: string, duration = 4000, priority = 4) {
  instance?.tipsMessage(text, duration, priority);
}

export function live2dShow() {
  if (container) container.style.display = "";
  void instance?.stageSlideIn();
}

export function live2dHide() {
  if (container) container.style.display = "none";
}
