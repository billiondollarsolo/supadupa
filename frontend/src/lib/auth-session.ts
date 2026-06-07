import { create } from "zustand";
import { clearToken, getToken, logoutSession, setToken } from "../api";

type AuthSessionStore = {
  token: string | null;
  setAuthenticated: (token: string) => void;
  logout: () => void;
};

export const useAuthSession = create<AuthSessionStore>((set) => ({
  token: getToken(),
  setAuthenticated: (token) => {
    setToken(token);
    set({ token });
  },
  logout: () => {
    void logoutSession().catch(() => undefined);
    clearToken();
    set({ token: null });
  },
}));
