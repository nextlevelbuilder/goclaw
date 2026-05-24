// Wire shape returned by bitrix.portals.list. Mirrors the Go view struct
// (internal/gateway/methods/bitrix_portals.go::bitrixPortalView). Credentials
// are never present here — they live encrypted in the database and are only
// readable by the gateway runtime.
export interface BitrixPortal {
  name: string;
  domain: string;
  installed: boolean;
  public_url: string;
  created_at: string;
}

// Input shape for bitrix.portals.create. Server validates name/domain regex
// and (tenant_id, name) uniqueness. Returns install_url the admin opens to
// authorize the app inside Bitrix24.
export interface BitrixPortalCreateInput {
  name: string;
  domain: string;
  client_id: string;
  client_secret: string;
}

export interface BitrixPortalCreateResult {
  name: string;
  domain: string;
  install_url: string;
  // Present when the gateway hasn't observed its public URL yet; UI shows it
  // as a banner so admin knows to retry get_install_url later.
  warning?: string;
}
