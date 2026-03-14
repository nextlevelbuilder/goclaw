import { create } from "zustand";
import { LOCAL_STORAGE_KEYS } from "@/lib/constants";

interface AuthState {
  token: string;
  userId: string;
  senderID: string; // browser pairing: persistent device identity
  keycloakToken: string; // Keycloak access token
  keycloakRefreshToken: string; // Keycloak refresh token
  displayName: string; // Keycloak user's full name
  connected: boolean;
  serverInfo: { name?: string; version?: string } | null;

  setCredentials: (token: string, userId: string) => void;
  setPairing: (senderID: string, userId: string) => void;
  setKeycloakAuth: (keycloakToken: string, userId: string, displayName?: string, refreshToken?: string) => void;
  setConnected: (connected: boolean, serverInfo?: { name?: string; version?: string }) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  token: localStorage.getItem(LOCAL_STORAGE_KEYS.TOKEN) ?? "",
  userId: localStorage.getItem(LOCAL_STORAGE_KEYS.USER_ID) ?? "",
  senderID: localStorage.getItem(LOCAL_STORAGE_KEYS.SENDER_ID) ?? "",
  keycloakToken: localStorage.getItem(LOCAL_STORAGE_KEYS.KEYCLOAK_TOKEN) ?? "",
  keycloakRefreshToken: localStorage.getItem(LOCAL_STORAGE_KEYS.KEYCLOAK_REFRESH_TOKEN) ?? "",
  displayName: localStorage.getItem(LOCAL_STORAGE_KEYS.DISPLAY_NAME) ?? "",
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

  setKeycloakAuth: (keycloakToken, userId, displayName, refreshToken) => {
    localStorage.setItem(LOCAL_STORAGE_KEYS.KEYCLOAK_TOKEN, keycloakToken);
    localStorage.setItem(LOCAL_STORAGE_KEYS.USER_ID, userId);
    // Also set as the gateway token so WS connect works
    localStorage.setItem(LOCAL_STORAGE_KEYS.TOKEN, keycloakToken);
    if (displayName) {
      localStorage.setItem(LOCAL_STORAGE_KEYS.DISPLAY_NAME, displayName);
    }
    if (refreshToken) {
      localStorage.setItem(LOCAL_STORAGE_KEYS.KEYCLOAK_REFRESH_TOKEN, refreshToken);
    }
    set({
      keycloakToken, userId, token: keycloakToken,
      displayName: displayName ?? "",
      keycloakRefreshToken: refreshToken ?? "",
    });
  },

  setConnected: (connected, serverInfo) => {
    set({ connected, serverInfo: serverInfo ?? null });
  },

  logout: () => {
    localStorage.removeItem(LOCAL_STORAGE_KEYS.TOKEN);
    localStorage.removeItem(LOCAL_STORAGE_KEYS.USER_ID);
    localStorage.removeItem(LOCAL_STORAGE_KEYS.SENDER_ID);
    localStorage.removeItem(LOCAL_STORAGE_KEYS.KEYCLOAK_TOKEN);
    localStorage.removeItem(LOCAL_STORAGE_KEYS.KEYCLOAK_REFRESH_TOKEN);
    localStorage.removeItem(LOCAL_STORAGE_KEYS.DISPLAY_NAME);
    set({ token: "", userId: "", senderID: "", keycloakToken: "", keycloakRefreshToken: "", displayName: "", connected: false, serverInfo: null });
  },
}));
