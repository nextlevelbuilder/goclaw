import { create } from "zustand";
import { LOCAL_STORAGE_KEYS } from "@/lib/constants";
import { clearSetupSkippedState } from "@/lib/setup-skip";
import type { TenantMembership } from "@/types/tenant";

type UserRole = "owner" | "admin" | "operator" | "viewer" | "";

interface AuthState {
  token: string;
  userId: string;
  senderID: string; // browser pairing: persistent device identity
  connected: boolean;
  role: UserRole; // server-assigned role from connect response
  serverInfo: { name?: string; version?: string } | null;
  tenantId: string;
  tenantName: string;
  tenantSlug: string;
  isOwner: boolean;
  availableTenants: TenantMembership[];
  tenantSelected: boolean; // true after user picks a tenant (or auto-selected)

  setCredentials: (token: string, userId: string) => void;
  setPairing: (senderID: string, userId: string) => void;
  setConnected: (connected: boolean, serverInfo?: { name?: string; version?: string }) => void;
  setRole: (role: UserRole) => void;
  setTenant: (id: string, name: string, slug: string, isOwner: boolean) => void;
  setAvailableTenants: (tenants: TenantMembership[]) => void;
  setTenantSelected: (selected: boolean) => void;
  logout: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  token: localStorage.getItem(LOCAL_STORAGE_KEYS.TOKEN) ?? "",
  userId: localStorage.getItem(LOCAL_STORAGE_KEYS.USER_ID) ?? "",
  senderID: localStorage.getItem(LOCAL_STORAGE_KEYS.SENDER_ID) ?? "",
  connected: false,
  role: "" as UserRole,
  serverInfo: null,
  tenantId: "",
  tenantName: "",
  tenantSlug: "",
  isOwner: false,
  availableTenants: [],
  tenantSelected: !!localStorage.getItem(LOCAL_STORAGE_KEYS.TENANT_ID),

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

  setRole: (role) => {
    set({ role });
  },

  setTenant: (id, name, slug, isOwner) => {
    set({ tenantId: id, tenantName: name, tenantSlug: slug, isOwner });
  },

  setAvailableTenants: (tenants) => {
    set({ availableTenants: tenants });
  },

  setTenantSelected: (selected) => {
    set({ tenantSelected: selected });
  },

  logout: () => {
    localStorage.removeItem(LOCAL_STORAGE_KEYS.TOKEN);
    localStorage.removeItem(LOCAL_STORAGE_KEYS.USER_ID);
    localStorage.removeItem(LOCAL_STORAGE_KEYS.SENDER_ID);
    localStorage.removeItem(LOCAL_STORAGE_KEYS.TENANT_ID);
    localStorage.removeItem(LOCAL_STORAGE_KEYS.TENANT_HINT);
    clearSetupSkippedState();
    set({
      token: "", userId: "", senderID: "", connected: false, role: "", serverInfo: null,
      tenantId: "", tenantName: "", tenantSlug: "", isOwner: false, availableTenants: [],
      tenantSelected: false,
    });
  },
}));
