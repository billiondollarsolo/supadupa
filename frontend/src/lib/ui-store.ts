import { create } from "zustand";
import type { ProjectTab } from "./project-config";

type UIStore = {
  paletteOpen: boolean;
  paletteQuery: string;
  projectSections: Partial<Record<ProjectTab, string>>;
  theme: "dark" | "light";
  toasts: ToastMessage[];
  // Keys of advisory hints/banners the user has dismissed (persisted), so we
  // don't nag them again across sessions.
  dismissedHints: Record<string, boolean>;
  openPalette: () => void;
  closePalette: () => void;
  setPaletteQuery: (query: string) => void;
  setProjectSection: (tab: ProjectTab, section: string) => void;
  setTheme: (theme: "dark" | "light") => void;
  dismissHint: (key: string) => void;
  addToast: (toast: Omit<ToastMessage, "id">) => void;
  removeToast: (id: string) => void;
};

type ToastMessage = {
  id: string;
  title: string;
  detail?: string;
  kind?: "success" | "warning" | "danger";
};

export const useUIStore = create<UIStore>((set) => ({
  paletteOpen: false,
  paletteQuery: "",
  projectSections: {},
  theme: initialTheme(),
  toasts: [],
  dismissedHints: initialDismissedHints(),
  openPalette: () => set({ paletteOpen: true, paletteQuery: "" }),
  closePalette: () => set({ paletteOpen: false, paletteQuery: "" }),
  setPaletteQuery: (paletteQuery) => set({ paletteQuery }),
  setProjectSection: (tab, section) => set((state) => ({ projectSections: { ...state.projectSections, [tab]: section } })),
  setTheme: (theme) => {
    localStorage.setItem("supadupa-theme", theme);
    set({ theme });
  },
  dismissHint: (key) =>
    set((state) => {
      const dismissedHints = { ...state.dismissedHints, [key]: true };
      try {
        localStorage.setItem("supadupa-dismissed-hints", JSON.stringify(dismissedHints));
      } catch {
        // Ignore storage failures; dismissal just won't persist across reloads.
      }
      return { dismissedHints };
    }),
  addToast: (toast) => {
    const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
    set((state) => ({ toasts: [...state.toasts, { ...toast, id }] }));
    // Auto-dismiss is owned by ToastHost, which varies the delay by kind
    // (danger toasts linger longer than success ones). No fixed timer here.
  },
  removeToast: (id) => set((state) => ({ toasts: state.toasts.filter((toast) => toast.id !== id) })),
}));

function initialTheme(): "dark" | "light" {
  const stored = localStorage.getItem("supadupa-theme");
  return stored === "light" ? "light" : "dark";
}

function initialDismissedHints(): Record<string, boolean> {
  try {
    const stored = localStorage.getItem("supadupa-dismissed-hints");
    if (!stored) return {};
    const parsed = JSON.parse(stored);
    return parsed && typeof parsed === "object" ? (parsed as Record<string, boolean>) : {};
  } catch {
    return {};
  }
}
