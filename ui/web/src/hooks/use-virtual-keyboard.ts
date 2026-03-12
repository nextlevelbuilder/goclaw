import { useEffect } from "react";

/**
 * Tracks virtual keyboard height via VisualViewport API and exposes it
 * as a CSS custom property `--keyboard-height` on the document root.
 * Components can use `var(--keyboard-height, 0px)` to offset content.
 *
 * Call once near the app root (e.g. in the chat page).
 */
export function useVirtualKeyboard() {
  useEffect(() => {
    const vv = window.visualViewport;
    if (!vv) return;

    const update = () => {
      const height = Math.max(0, window.innerHeight - vv.height);
      document.documentElement.style.setProperty("--keyboard-height", `${height}px`);
    };

    vv.addEventListener("resize", update);
    vv.addEventListener("scroll", update);
    return () => {
      vv.removeEventListener("resize", update);
      vv.removeEventListener("scroll", update);
      document.documentElement.style.removeProperty("--keyboard-height");
    };
  }, []);
}
