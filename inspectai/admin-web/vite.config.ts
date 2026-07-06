import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// 开发时代理到本地 Go 后端(18080);构建产物走 nginx 同源部署
export default defineConfig({
  plugins: [react()],
  base: "./",
  server: {
    port: 18090,
    proxy: {
      "/api": "http://127.0.0.1:18080",
      "/health": "http://127.0.0.1:18080",
      "/uploads": "http://127.0.0.1:18080",
    },
  },
});
