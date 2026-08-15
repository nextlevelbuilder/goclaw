import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./i18n";
import App from "./App";
import "./index.css";
import { ApiError } from "@/api/errors";

/**
 * Global safety net for unhandled promise rejections from API calls.
 *
 * Every data-fetching hook in this app (see src/pages/*\/hooks/*.ts) already
 * catches `ApiError`s, shows a `toast.error(...)` with a user-friendly message,
 * and re-throws so the calling component can react locally (e.g. keep a form
 * open, avoid resetting "dirty" state on failure). Some call sites forget to
 * `catch` that re-thrown promise, which produces a noisy, user-invisible
 * "Uncaught (in promise) ApiError" in the console even though the user already
 * saw a toast for the underlying failure.
 *
 * This handler does NOT show its own toast (that would double up with the one
 * already shown by the originating hook) — it only suppresses the redundant
 * browser-level "unhandled rejection" noise for errors known to originate from
 * our API layer, which have already been surfaced to the user via toast. It
 * never calls `preventDefault()` for non-`ApiError` rejections, so genuine
 * unexpected bugs still surface normally (console + any error-reporting tooling).
 */
window.addEventListener("unhandledrejection", (event) => {
  if (event.reason instanceof ApiError) {
    event.preventDefault();
  }
});

const LOADER_MIN_MS = 800;
const loaderStart = performance.now();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App />
  </StrictMode>,
);

const ric = window.requestIdleCallback ?? ((cb: () => void) => setTimeout(cb, 1));
ric(() => {
  const elapsed = performance.now() - loaderStart;
  const delay = Math.max(0, LOADER_MIN_MS - elapsed);
  setTimeout(() => {
    const loader = document.getElementById("app-loader");
    if (loader) {
      loader.classList.add("fade-out");
      setTimeout(() => loader.remove(), 300);
    }
  }, delay);
});
