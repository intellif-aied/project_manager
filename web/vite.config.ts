import { fileURLToPath, URL } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const hmrHost = env.VITE_DEV_HMR_HOST ?? "192.168.28.25";
  const apiTarget = env.VITE_DEV_API_TARGET ?? "http://127.0.0.1:18090";
  const devPort = Number(env.VITE_DEV_PORT ?? 5173);

  return {
    plugins: [react()],
    resolve: {
      alias: {
        "@": "/src",
        "@toast-ui/editor/viewer": fileURLToPath(
          new URL("./node_modules/@toast-ui/editor/dist/esm/indexViewer.js", import.meta.url)
        ),
        "@toast-ui/editor/toastui-editor-viewer.css": fileURLToPath(
          new URL("./node_modules/@toast-ui/editor/dist/toastui-editor-viewer.css", import.meta.url)
        )
      }
    },
    server: {
      host: "0.0.0.0",
      port: devPort,
      strictPort: true,
      allowedHosts: true,
      hmr: {
        host: hmrHost
      },
      proxy: {
        "/api/v1": {
          target: apiTarget,
          changeOrigin: true
        }
      }
    },
    preview: {
      host: "0.0.0.0",
      port: 4173
    },
    build: {
      chunkSizeWarningLimit: 1300,
      rollupOptions: {
        output: {
          manualChunks: (id) => {
            if (
              id.includes("node_modules/react") ||
              id.includes("node_modules/react-dom") ||
              id.includes("node_modules/react-router")
            ) {
              return "react";
            }
            if (id.includes("node_modules/antd") || id.includes("node_modules/@ant-design")) {
              return "antd";
            }
            if (id.includes("node_modules/@tanstack")) {
              return "query";
            }
            return undefined;
          }
        }
      }
    }
  };
});
