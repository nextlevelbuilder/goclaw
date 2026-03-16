export interface GatewayUserData {
  id: string;
  user_id: string;
  gateway_token?: string; // only returned on create
  token_hint?: string;    // masked preview: first4...last4
  role: "root" | "admin";
  created_at: string;
}

export interface GatewayUserCreateInput {
  user_id: string;
}

export interface GatewayUserCreateResponse extends GatewayUserData {
  gateway_token: string; // shown only once
}
