import { create } from "zustand";
import { LOCAL_STORAGE_KEYS } from "@/lib/constants";

interface AuthState {
  token: string;
  userId: string;
  senderID: string; // browser pairing: persistent device identity
  keycloakToken: string; // Keycloak access token
  displayName: string; // Keycloak user's full name
  connected: boolean;
  serverInfo: { name?: string; version?: string } | null;

  setCredentials: (token: string, userId: string) => void;
  setPairing: (senderID: string, userId: string) => void;
  setKeycloakAuth: (keycloakToken: string, userId: string, displayName?: string) => void;
  setConnected: (connected: boolean, serverInfo?: { name?: string; version?: string }) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  token: localStorage.getItem(LOCAL_STORAGE_KEYS.TOKEN) ?? "",
  userId: localStorage.getItem(LOCAL_STORAGE_KEYS.USER_ID) ?? "",
  senderID: localStorage.getItem(LOCAL_STORAGE_KEYS.SENDER_ID) ?? "",
  keycloakToken: localStorage.getItem("goclaw:keycloakToken") ?? "",
  displayName: localStorage.getItem("goclaw:displayName") ?? "",
  connected: false,
  serverInfo: null,

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

  setKeycloakAuth: (keycloakToken, userId, displayName) => {
    localStorage.setItem("goclaw:keycloakToken", keycloakToken);
    localStorage.setItem(LOCAL_STORAGE_KEYS.USER_ID, userId);
    // Also set as the gateway token so WS connect works
    localStorage.setItem(LOCAL_STORAGE_KEYS.TOKEN, keycloakToken);
    if (displayName) {
      localStorage.setItem("goclaw:displayName", displayName);
    }
    set({ keycloakToken, userId, token: keycloakToken, displayName: displayName ?? "" });
  },

  setConnected: (connected, serverInfo) => {
    set({ connected, serverInfo: serverInfo ?? null });
  },

  logout: () => {
    localStorage.removeItem(LOCAL_STORAGE_KEYS.TOKEN);
    localStorage.removeItem(LOCAL_STORAGE_KEYS.USER_ID);
    localStorage.removeItem(LOCAL_STORAGE_KEYS.SENDER_ID);
    localStorage.removeItem("goclaw:keycloakToken");
    localStorage.removeItem("goclaw:displayName");
    set({ token: "", userId: "", senderID: "", keycloakToken: "", displayName: "", connected: false, serverInfo: null });
  },
}));
