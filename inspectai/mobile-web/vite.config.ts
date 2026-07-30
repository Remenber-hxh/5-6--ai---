import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// 移动端新前端(mobile-web)。与旧版 frontend/ 并存,共用后端 /api。
// dev 端口 18091(admin-web 占 18090),代理到本地 Go 后端 18080。
export default defineConfig({
  plugins: [react()],
  base: "./",
  resolve: {
    alias: { "@": "/src" },
  },
  build: {
    rollupOptions: {
      output: {
        // 按需引入走的是子路径(esm/toast 等),整包名匹配不到,
        // 故用函数式按路径分块
        manualChunks(id) {
          // @babel/runtime 的 helper(_extends 等)被 arco 和 react-transition-group 共用。
          // 不单独切出来的话 Rollup 会把它塞进 arco 块,react 块再回头引用它 ——
          // 于是 arco → react → arco,构建报 "Circular chunk"。让共用件自己成块即可。
          if (id.includes("node_modules/@babel/runtime")) return "babel-helpers";
          if (id.includes("@arco-design")) return "arco";
          if (id.includes("node_modules/react")) return "react";
        },
      },
    },
  },
  server: {
    port: 18091,
    proxy: {
      "/api": "http://127.0.0.1:18080",
      "/health": "http://127.0.0.1:18080",
      "/uploads": "http://127.0.0.1:18080",
      "/storage": "http://127.0.0.1:18080",
    },
  },
});
