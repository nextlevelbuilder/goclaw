import { useEffect } from "react";
import { BrowserRouter } from "react-router";
import { AppProviders } from "@/components/providers/app-providers";
import { AppRoutes } from "@/routes";
import { useBrandingStore } from "@/stores/use-branding-store";

export default function App() {
  const appName = useBrandingStore((s) => s.appName);
  const setBranding = useBrandingStore((s) => s.setBranding);

  // Fetch branding from public endpoint on mount (no auth required).
  // This ensures login page, setup page, and document title all reflect
  // the configured app name even before WebSocket connection is established.
  useEffect(() => {
    fetch("/v1/configs")
      .then((r) => (r.ok ? r.json() : null))
      .then((data) => {
        if (data?.app_name) {
          setBranding(data.app_name, data.app_description ?? "");
        }
      })
      .catch(() => {
        // Silently ignore — falls back to default "GoClaw"
      });
  }, [setBranding]);

  useEffect(() => {
    document.title = `${appName} Dashboard`;
  }, [appName]);

  return (
    <BrowserRouter>
      <AppProviders>
        <AppRoutes />
      </AppProviders>
    </BrowserRouter>
  );
}
