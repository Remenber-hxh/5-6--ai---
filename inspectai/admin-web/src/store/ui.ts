import { create } from "zustand";

// 全局项目筛选(与旧版 selectedProject 等价):顶栏选择,各业务页应用
interface UiState {
  project: string;
  setProject: (p: string) => void;
}

export const useUi = create<UiState>((set) => ({
  project: "",
  setProject: (project) => set({ project }),
}));

export function matchProject(project: string, itemProject?: string): boolean {
  return !project || (itemProject || "") === project;
}
