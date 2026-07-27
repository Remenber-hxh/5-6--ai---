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
        manualChunks: {
          antd: ["antd-mobile"],
          react: ["react", "react-dom", "react-router-dom"],
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
