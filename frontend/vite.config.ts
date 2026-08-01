/// <reference types="vitest/config" />
import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, "..", "");
  const allowedHosts = [process.env.SUPADUPA_ADMIN_HOST, env.SUPADUPA_ADMIN_HOST].filter(Boolean) as string[];

  return {
    plugins: [react(), tailwindcss()],
    // Vitest config (plan L8): coverage floor for unit-tested admin UI modules.
    test: {
      coverage: {
        provider: "v8",
        reporter: ["text", "json-summary"],
        include: ["src/lib/**/*.{ts,tsx}", "src/components/**/*.{ts,tsx}"],
        exclude: ["**/*.test.{ts,tsx}", "**/vite-env.d.ts"],
        thresholds: {
          // Floor is intentionally modest: suite focuses on lib/components with
          // real tests (routes, validators, modal, error boundary, reveal-field).
          // Raise as page-level coverage lands.
          lines: 20,
          functions: 20,
          statements: 20,
          branches: 15,
        },
      },
    },
    build: {
      rollupOptions: {
        output: {
          manualChunks(id) {
            if (!id.includes("node_modules")) {
              return undefined;
            }
            if (id.includes("/react/") || id.includes("/react-dom/") || id.includes("/scheduler/")) {
              return "vendor-react";
            }
            if (id.includes("/@tanstack/")) {
              return "vendor-tanstack";
            }
            if (id.includes("/lucide-react/")) {
              return "vendor-icons";
            }
            if (id.includes("/recharts/") || id.includes("/d3-") || id.includes("/victory-vendor/")) {
              return "vendor-charts";
            }
            if (id.includes("/zustand/")) {
              return "vendor-state";
            }
            return "vendor";
          },
        },
      },
    },
    server: {
      port: 3000,
      allowedHosts,
    },
  };
});
