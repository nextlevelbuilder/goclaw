import { useState, useEffect } from "react";
import { useNavigate, useLocation } from "react-router";
import { useTranslation } from "react-i18next";
import { useAuthStore } from "@/stores/use-auth-store";
import { ROUTES } from "@/lib/constants";
import { LoginLayout } from "./login-layout";
import { LoginTabs, type LoginMode } from "./login-tabs";
import { TokenForm } from "./token-form";
import { PairingForm } from "./pairing-form";
import { KeycloakForm } from "./keycloak-form";

export function LoginPage() {
  const { t } = useTranslation("login");
  const [mode, setMode] = useState<LoginMode>("token");

  const setCredentials = useAuthStore((s) => s.setCredentials);
  const setPairing = useAuthStore((s) => s.setPairing);
  const setKeycloakAuth = useAuthStore((s) => s.setKeycloakAuth);
  const navigate = useNavigate();
  const location = useLocation();

  const from =
    (location.state as { from?: { pathname: string } })?.from?.pathname ??
    ROUTES.OVERVIEW;

  // Detect Keycloak OAuth callback on mount (code + state in URL params)
  useEffect(() => {
    const params = new URLSearchParams(window.location.search);
    const code = params.get("code");
    const state = params.get("state");
    const storedState = sessionStorage.getItem("kc_state");

    if (code && state && state === storedState) {
      // Clear URL params immediately
      window.history.replaceState({}, "", window.location.pathname);
      // Switch to Keycloak tab and process the callback
      setMode("keycloak");
      processKeycloakCallback(code);
    }
  }, []);

  async function processKeycloakCallback(code: string) {
    try {
      // Fetch Keycloak config
      const configRes = await fetch("/v1/auth/keycloak/config");
      if (!configRes.ok) throw new Error("Failed to fetch Keycloak config");
      const kcConfig = await configRes.json();

      const tokenUrl = `${kcConfig.url}/realms/${kcConfig.realm}/protocol/openid-connect/token`;
      const codeVerifier = sessionStorage.getItem("kc_code_verifier") || "";
      const redirectUri = `${window.location.origin}/login`;

      // Exchange code for tokens
      const tokenRes = await fetch(tokenUrl, {
        method: "POST",
        headers: { "Content-Type": "application/x-www-form-urlencoded" },
        body: new URLSearchParams({
          grant_type: "authorization_code",
          client_id: kcConfig.client_id,
          code,
          redirect_uri: redirectUri,
          code_verifier: codeVerifier,
        }),
      });

      if (!tokenRes.ok) {
        const errBody = await tokenRes.text();
        throw new Error(`Token exchange failed: ${errBody}`);
      }

      const tokens = await tokenRes.json();
      const accessToken = tokens.access_token;

      // Clean up PKCE state
      sessionStorage.removeItem("kc_state");
      sessionStorage.removeItem("kc_code_verifier");

      // Call /me and complete login
      await handleKeycloakLogin(accessToken);
    } catch (err) {
      console.error("Keycloak callback failed:", err);
    }
  }

  function handleTokenLogin(userId: string, token: string) {
    setCredentials(token, userId);
    navigate(from, { replace: true });
  }

  function handlePairingApproved(senderID: string, userId: string) {
    setPairing(senderID, userId);
    setTimeout(() => navigate(from, { replace: true }), 500);
  }

  async function handleKeycloakLogin(accessToken: string) {
    try {
      const res = await fetch("/v1/auth/keycloak/me", {
        headers: { Authorization: `Bearer ${accessToken}` },
      });
      if (!res.ok) throw new Error(`/me returned ${res.status}`);
      const userInfo = await res.json();

      setKeycloakAuth(accessToken, userInfo.id, userInfo.username || userInfo.email || userInfo.id);
      navigate(from, { replace: true });
    } catch (err) {
      console.error("Keycloak /me call failed:", err);
    }
  }

  return (
    <LoginLayout subtitle={t("subtitle")}>
      <LoginTabs mode={mode} onModeChange={setMode} />
      {mode === "token" ? (
        <TokenForm onSubmit={handleTokenLogin} />
      ) : mode === "pairing" ? (
        <PairingForm onApproved={handlePairingApproved} />
      ) : (
        <KeycloakForm onLogin={handleKeycloakLogin} />
      )}
    </LoginLayout>
  );
}
