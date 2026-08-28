import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    port: 5180,
    // Everything the panel talks to lives behind the orchestrator, including the
    // event socket and the desktop proxy, so one proxy target covers dev.
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: true, ws: true },
      "/vnc": { target: "http://localhost:8080", changeOrigin: true, ws: true },
      "/healthz": { target: "http://localhost:8080", changeOrigin: true },
    },
  },
  build: { outDir: "dist", sourcemap: true },
});
