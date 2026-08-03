import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/v1": "http://localhost:58080",
      "/api": "http://localhost:58080",
      "/health": "http://localhost:58080",
    },
  },
});
