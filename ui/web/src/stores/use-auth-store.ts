import { create } from "zustand";
import { LOCAL_STORAGE_KEYS } from "@/lib/constants";

interface AuthState {
  token: string;
  userId: string;
  senderID: string; // browser pairing: persistent device identity
  connected: boolean;
  serverInfo: { name?: string; version?: string } | null;
  role: string;

  setCredentials: (token: string, userId: string) => void;
  setPairing: (senderID: string, userId: string) => void;
  setConnected: (connected: boolean, serverInfo?: { name?: string; version?: string }) => void;
  setRole: (role: string) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  token: localStorage.getItem(LOCAL_STORAGE_KEYS.TOKEN) ?? "",
  userId: localStorage.getItem(LOCAL_STORAGE_KEYS.USER_ID) ?? "",
  senderID: localStorage.getItem(LOCAL_STORAGE_KEYS.SENDER_ID) ?? "",
  connected: false,
  serverInfo: null,
  role: "",

  setCredentials: (token, userId) => {
    localStorage.setItem(LOCAL_STORAGE_KEYS.TOKEN, token);
    localStorage.setItem(LOCAL_STORAGE_KEYS.USER_ID, userId);
    set({ token, userId });
  },

  setPairing: (senderID, userId) => {
    localStorage.setItem(LOCAL_STORAGE_KEYS.SENDER_ID, senderID);
    localStorage.setItem(LOCAL_STORAGE_KEYS.USER_ID, userId);
    set({ senderID, userId });
  },

  setConnected: (connected, serverInfo) => {
    set({ connected, serverInfo: serverInfo ?? null });
  },

  setRole: (role) => set({ role }),

  logout: () => {
    localStorage.removeItem(LOCAL_STORAGE_KEYS.TOKEN);
    localStorage.removeItem(LOCAL_STORAGE_KEYS.USER_ID);
    localStorage.removeItem(LOCAL_STORAGE_KEYS.SENDER_ID);
    set({ token: "", userId: "", senderID: "", connected: false, serverInfo: null, role: "" });
  },
}));
