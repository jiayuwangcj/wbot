import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  base: "/ui/",
  plugins: [react()],
  publicDir: "public",
  build: {
    outDir: "../internal/webui/web/dist",
    emptyOutDir: true,
    rollupOptions: {
      input: {
        dashboard: "index.html",
        watchlist: "watchlist.html",
        results: "results.html",
        data: "data.html",
        admin: "admin.html",
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      "/v1": "http://127.0.0.1:8080",
    },
  },
});
