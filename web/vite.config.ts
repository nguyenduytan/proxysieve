import { defineConfig } from "vite";

// M0 tooling smoke build only. The API-driven SPA is implemented in M12.
export default defineConfig({
  build: {
    lib: {
      entry: "src/product.ts",
      formats: ["es"],
      fileName: "proxysieve-bootstrap",
    },
  },
});
