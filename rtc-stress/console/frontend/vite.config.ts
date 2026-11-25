import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig(({ mode }) => {
  const target =
    loadEnv(mode, ".", "VITE_").VITE_API_TARGET ||
    "http://114.132.190.201:8092";
  return {
    plugins: [react()],
    server: {
      port: 5174,
      strictPort: true,
      proxy: {
        "/api": { target, changeOrigin: false },
        "/healthz": { target, changeOrigin: false },
      },
    },
  };
});
