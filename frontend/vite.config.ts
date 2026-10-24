import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Geliştirme sunucusunda /api isteği yerel Go API'ye yönlendirilir; üretimde aynı işi nginx yapar.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 3000,
    proxy: {
      "/api": "http://localhost:8080",
      "/health": "http://localhost:8080",
    },
  },
});
