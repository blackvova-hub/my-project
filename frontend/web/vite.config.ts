import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { fileURLToPath } from "node:url";

export default defineConfig({
  plugins: [react(), ...tailwindcss()],
  worker: { format: "es" },
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
    dedupe: ["react", "react-dom"],
  },
  server: {
    host: true,
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": {
        target: process.env.DEV_API_PROXY || "http://127.0.0.1:8080",
        changeOrigin: true,
      },
      "/auth": {
        target: process.env.DEV_API_PROXY || "http://127.0.0.1:8080",
        changeOrigin: true,
      },
    },
  },
});
