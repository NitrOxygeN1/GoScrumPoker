import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const backend = "http://127.0.0.1:8080";

/** Public HTML pages served at extensionless paths (matches production). */
const staticPageRewrites = {
  "/privacy": "/privacy.html",
  "/terms": "/terms.html",
  "/support": "/support.html",
  "/help": "/help.html",
  "/draft-opt-out": "/draft-opt-out.html",
  "/meet-debug": "/meet-debug.html",
};

/** Serves `index.html` for client routes like `/<room-uuid>` in dev and `vite preview`. */
function installSpaFallback() {
  return (server) => {
    return () => {
      server.middlewares.use((req, res, next) => {
        if (req.method !== "GET" && req.method !== "HEAD") return next();
        const u = new URL(req.url, "http://v.local");
        if (staticPageRewrites[u.pathname]) {
          u.pathname = staticPageRewrites[u.pathname];
          req.url = u.pathname + u.search;
        }
        const p = u.pathname;
        if (p.startsWith("/@") || p.startsWith("/node_modules/") || p.startsWith("/.vite/")) {
          return next();
        }
        if (p.startsWith("/src/") || p.startsWith("/@fs/")) return next();
        if (p.startsWith("/assets/") || p.startsWith("/static/")) return next();
        if (
          p === "/privacy" ||
          p === "/terms" ||
          p === "/support" ||
          p === "/help" ||
          p === "/draft-opt-out" ||
          p === "/meet-debug"
        ) {
          return next();
        }
        if (p === "/") return next();
        if (
          p.startsWith("/rooms") ||
          p.startsWith("/ws") ||
          p.startsWith("/auth/") ||
          p.startsWith("/api/") ||
          p === "/health" ||
          p === "/logout"
        ) {
          return next();
        }
        if (/\.[a-zA-Z0-9][\w+.-]*$/.test(p.split("/").pop() || "")) {
          return next();
        }
        u.pathname = "/";
        req.url = u.pathname + u.search;
        next();
      });
    };
  };
}

function spaIndexFallback() {
  return {
    name: "spa-index-fallback",
    configureServer: installSpaFallback(),
    configurePreviewServer: installSpaFallback(),
  };
}

export default defineConfig({
  base: "/",
  plugins: [react(), spaIndexFallback()],
  server: {
    port: 5173,
    proxy: {
      "/rooms": { target: backend, changeOrigin: true },
      "/ws": { target: backend, ws: true, changeOrigin: true },
      "/health": { target: backend, changeOrigin: true },
      "/api": { target: backend, changeOrigin: true },
      "/auth": { target: backend, changeOrigin: true },
      "/logout": { target: backend, changeOrigin: true },
    },
  },
});
