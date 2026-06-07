import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, "..", "");
  const allowedHosts = [process.env.SUPADUPA_ADMIN_HOST, env.SUPADUPA_ADMIN_HOST].filter(Boolean) as string[];

  return {
    plugins: [react(), tailwindcss()],
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
