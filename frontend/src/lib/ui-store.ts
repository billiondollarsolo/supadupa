import { create } from "zustand";
import type { ProjectTab } from "./project-config";

type UIStore = {
  paletteOpen: boolean;
  paletteQuery: string;
  projectSections: Partial<Record<ProjectTab, string>>;
  theme: "dark" | "light";
  toasts: ToastMessage[];
  openPalette: () => void;
  closePalette: () => void;
  setPaletteQuery: (query: string) => void;
  setProjectSection: (tab: ProjectTab, section: string) => void;
  setTheme: (theme: "dark" | "light") => void;
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
  openPalette: () => set({ paletteOpen: true, paletteQuery: "" }),
  closePalette: () => set({ paletteOpen: false, paletteQuery: "" }),
  setPaletteQuery: (paletteQuery) => set({ paletteQuery }),
  setProjectSection: (tab, section) => set((state) => ({ projectSections: { ...state.projectSections, [tab]: section } })),
  setTheme: (theme) => {
    localStorage.setItem("supadupa-theme", theme);
    set({ theme });
  },
  addToast: (toast) => {
    const id = `${Date.now()}-${Math.random().toString(36).slice(2)}`;
    set((state) => ({ toasts: [...state.toasts, { ...toast, id }] }));
    window.setTimeout(() => {
      set((state) => ({ toasts: state.toasts.filter((item) => item.id !== id) }));
    }, 3600);
  },
  removeToast: (id) => set((state) => ({ toasts: state.toasts.filter((toast) => toast.id !== id) })),
}));

function initialTheme(): "dark" | "light" {
  const stored = localStorage.getItem("supadupa-theme");
  return stored === "light" ? "light" : "dark";
}
