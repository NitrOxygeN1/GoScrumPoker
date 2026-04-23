import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const backend = "http://127.0.0.1:8080";

export default defineConfig({
  base: "/",
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/rooms": { target: backend, changeOrigin: true },
      "/ws": { target: backend, ws: true, changeOrigin: true },
    },
  },
});
