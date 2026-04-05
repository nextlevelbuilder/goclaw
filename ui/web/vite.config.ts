import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "path";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const backendPort = env.VITE_BACKEND_PORT || "9600";
  const backendHost = env.VITE_BACKEND_HOST || "localhost";

  return {
    plugins: [react(), tailwindcss()],
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "./src"),
      },
    },
    server: {
      port: 5173,
      proxy: {
        "/ws": {
          target: `http://${backendHost}:${backendPort}`,
          ws: true,
          changeOrigin: true,
        },
        "/v1": {
          target: `http://${backendHost}:${backendPort}`,
          changeOrigin: true,
        },
        "/health": {
          target: `http://${backendHost}:${backendPort}`,
          changeOrigin: true,
        },
        "/browser/status": {
          target: `http://${backendHost}:${backendPort}`,
          changeOrigin: true,
        },
        "/browser/tabs": {
          target: `http://${backendHost}:${backendPort}`,
          changeOrigin: true,
        },
        "/browser/start": {
          target: `http://${backendHost}:${backendPort}`,
          changeOrigin: true,
        },
        "/browser/stop": {
          target: `http://${backendHost}:${backendPort}`,
          changeOrigin: true,
        },
        // Authenticated screencast WS — direct connection from chat panel
        "/browser/screencast": {
          target: `http://${backendHost}:${backendPort}`,
          changeOrigin: true,
          ws: true,
        },
        // Proxy live view API/WS endpoints to backend.
        // The HTML page route /browser/live/:token is handled by React SPA (BrowserSharePage).
        "/browser/live": {
          target: `http://${backendHost}:${backendPort}`,
          changeOrigin: true,
          ws: true,
          // Only proxy API calls, not the SPA page
          bypass(req) {
            const p = req.url || "";
            // Allow: POST /browser/live (create session)
            if (req.method === "POST" && p === "/browser/live") return undefined;
            // Allow: /browser/live/{token}/ws, /browser/live/{token}/info
            if (p.match(/\/browser\/live\/[^/]+\/(ws|info)$/)) return undefined;
            // Block: GET /browser/live/{token} — let React SPA handle it
            return req.url;
          },
        },
      },
    },
    build: {
      outDir: "dist",
      emptyOutDir: true,
    },
  };
});
