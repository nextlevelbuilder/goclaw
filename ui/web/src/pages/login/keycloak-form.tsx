import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Shield } from "lucide-react";

interface KeycloakFormProps {
    onLogin: (accessToken: string) => void;
}

// PKCE helpers
function generateRandomString(length: number): string {
    const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~";
    const values = crypto.getRandomValues(new Uint8Array(length));
    return Array.from(values, (v) => chars[v % chars.length]).join("");
}

async function sha256(plain: string): Promise<ArrayBuffer> {
    const encoder = new TextEncoder();
    return crypto.subtle.digest("SHA-256", encoder.encode(plain));
}

function base64UrlEncode(buf: ArrayBuffer): string {
    return btoa(String.fromCharCode(...new Uint8Array(buf)))
        .replace(/\+/g, "-")
        .replace(/\//g, "_")
        .replace(/=+$/, "");
}

export function KeycloakForm({ onLogin: _onLogin }: KeycloakFormProps) {
    const { t } = useTranslation("login");
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string | null>(null);

    async function handleLoginClick() {
        setLoading(true);
        setError(null);
        try {
            // Fetch Keycloak config from backend
            const configRes = await fetch("/v1/auth/keycloak/config");
            if (!configRes.ok) throw new Error("Failed to fetch Keycloak config");
            const kcConfig = await configRes.json();

            // PKCE: generate code verifier and challenge
            const codeVerifier = generateRandomString(64);
            const codeChallenge = base64UrlEncode(await sha256(codeVerifier));
            const state = generateRandomString(32);

            // Store PKCE state
            sessionStorage.setItem("kc_state", state);
            sessionStorage.setItem("kc_code_verifier", codeVerifier);

            const redirectUri = `${window.location.origin}/login`;

            const authUrl = new URL(
                `${kcConfig.url}/realms/${kcConfig.realm}/protocol/openid-connect/auth`
            );
            authUrl.searchParams.set("client_id", kcConfig.client_id);
            authUrl.searchParams.set("response_type", "code");
            authUrl.searchParams.set("scope", "openid profile email");
            authUrl.searchParams.set("redirect_uri", redirectUri);
            authUrl.searchParams.set("state", state);
            authUrl.searchParams.set("code_challenge", codeChallenge);
            authUrl.searchParams.set("code_challenge_method", "S256");

            window.location.href = authUrl.toString();
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
            setLoading(false);
        }
    }

    return (
        <div className="space-y-4">
            {error && (
                <div className="rounded-md bg-destructive/10 p-3 text-sm text-destructive">
                    {error}
                </div>
            )}
            <button
                type="button"
                onClick={handleLoginClick}
                disabled={loading}
                className="flex w-full items-center justify-center gap-2 rounded-md bg-primary px-4 py-2.5 text-sm font-medium text-primary-foreground transition-colors hover:bg-primary/90 disabled:opacity-50"
            >
                <Shield className="h-4 w-4" />
                {loading ? t("keycloak.loggingIn") : t("keycloak.loginButton")}
            </button>
        </div>
    );
}
