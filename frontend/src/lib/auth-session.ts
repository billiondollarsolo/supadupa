import { create } from "zustand";
import { logoutSession } from "../api";
import type { User } from "../types";

type AuthSessionStore = {
  user: User | null;
  setAuthenticated: (user: User) => void;
  setUnauthenticated: () => void;
  logout: () => Promise<void>;
};

export const useAuthSession = create<AuthSessionStore>((set) => ({
  user: null,
  setAuthenticated: (user) => {
    set({ user });
  },
  setUnauthenticated: () => {
    set({ user: null });
  },
  logout: async () => {
    try {
      await logoutSession();
    } finally {
      set({ user: null });
    }
  },
}));
