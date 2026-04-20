import { useEffect, type RefObject } from "react";

interface Options {
  /** Whether the dropdown is currently open. */
  open: boolean;
  /** Invoked when an outside interaction should close the dropdown. */
  onClose: () => void;
  /**
   * Refs whose DOM subtree is considered "inside". Events originating inside
   * any of these refs do NOT trigger close. Typically [triggerRef, dropdownRef].
   */
  ignore: Array<RefObject<HTMLElement | null>>;
  /**
   * Close when the page scrolls OUTSIDE the dropdown. Scrolls that originate
   * inside the dropdown's own scrollable list are ignored — otherwise touch-
   * scrolling a long list would close the popup. Default: true.
   */
  closeOnOutsideScroll?: boolean;
  /** Close on window resize (position would be stale). Default: true. */
  closeOnResize?: boolean;
  /** Close on Escape key. Default: true. */
  closeOnEscape?: boolean;
}

/**
 * Standard outside-close behavior for portaled dropdowns. Mirrors the web
 * implementation at ui/web/src/hooks/use-portal-dropdown-close.ts.
 *
 * - `pointerdown` unifies mouse + touch + pen.
 * - `composedPath()` beats `Node.contains()` for portal subtrees.
 * - Listener install is deferred one tick so the click that OPENED the
 *   dropdown cannot be re-delivered and immediately close it (touch bug).
 * - Scroll listener ignores scrolls inside the dropdown (lets long lists
 *   scroll without self-closing).
 */
export function usePortalDropdownClose({
  open,
  onClose,
  ignore,
  closeOnOutsideScroll = true,
  closeOnResize = true,
  closeOnEscape = true,
}: Options) {
  useEffect(() => {
    if (!open) return;

    const isInside = (event: Event): boolean => {
      const nodes = ignore
        .map((r) => r.current)
        .filter((n): n is HTMLElement => n != null);
      const path = typeof event.composedPath === "function" ? event.composedPath() : [];
      if (path.length > 0) {
        return path.some((n) => nodes.includes(n as HTMLElement));
      }
      const target = event.target as Node | null;
      if (!target) return false;
      return nodes.some((n) => n.contains(target));
    };

    const handlePointerDown = (event: PointerEvent) => {
      if (!isInside(event)) onClose();
    };
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    const handleScroll = (event: Event) => {
      if (!isInside(event)) onClose();
    };
    const handleResize = () => onClose();

    const installId = window.setTimeout(() => {
      document.addEventListener("pointerdown", handlePointerDown);
      if (closeOnEscape) document.addEventListener("keydown", handleEscape);
      if (closeOnOutsideScroll) window.addEventListener("scroll", handleScroll, true);
      if (closeOnResize) window.addEventListener("resize", handleResize);
    }, 0);

    return () => {
      window.clearTimeout(installId);
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleEscape);
      window.removeEventListener("scroll", handleScroll, true);
      window.removeEventListener("resize", handleResize);
    };
  }, [open, onClose, ignore, closeOnOutsideScroll, closeOnResize, closeOnEscape]);
}
