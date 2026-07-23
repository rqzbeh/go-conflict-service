import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "../internal/conflict/static/dist",
    emptyOutDir: true,
    sourcemap: false,
  },
});
