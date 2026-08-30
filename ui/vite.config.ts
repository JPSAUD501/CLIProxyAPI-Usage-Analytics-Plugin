import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import { viteSingleFile } from "vite-plugin-singlefile";

export default defineConfig({
  plugins: [react(), viteSingleFile()],
  build: { outDir: "../internal/analytics", emptyOutDir: false, rollupOptions: { input: "dashboard.source.html", output: { entryFileNames: "dashboard.js" } } }
});
