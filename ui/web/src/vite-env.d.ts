/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_BACKEND_PORT?: string;
  readonly VITE_BACKEND_HOST?: string;
  readonly VITE_WS_URL?: string;
  readonly VITE_BRAND_APP_NAME?: string;
  readonly VITE_BRAND_APP_KEY?: string;
  readonly VITE_BRAND_TAGLINE?: string;
  readonly VITE_BRAND_LOGO_URL?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
