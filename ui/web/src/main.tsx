import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "./i18n";
import App from "./App";
import "./index.css";

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
