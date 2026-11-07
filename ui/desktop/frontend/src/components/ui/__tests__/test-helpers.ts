import i18n from '../../../i18n'

/**
 * jsdom lacks a few browser APIs that Radix (popper, focus scope, pointer
 * capture) and the theme code touch. Install minimal stand-ins once per file.
 */
export function installDomPolyfills(): void {
  if (!('ResizeObserver' in globalThis)) {
    class ResizeObserverStub {
      observe() {}
      unobserve() {}
      disconnect() {}
    }
    Object.defineProperty(globalThis, 'ResizeObserver', { configurable: true, value: ResizeObserverStub })
  }
  const proto = Element.prototype as unknown as Record<string, unknown>
  proto.scrollIntoView ??= function scrollIntoView() {}
  proto.hasPointerCapture ??= function hasPointerCapture() { return false }
  proto.releasePointerCapture ??= function releasePointerCapture() {}
  if (typeof window.matchMedia !== 'function') setPrefersDark(false)
}

/** Replace window.matchMedia with one that reports the given colour-scheme preference. */
export function setPrefersDark(dark: boolean): Set<(e: MediaQueryListEvent) => void> {
  const listeners = new Set<(e: MediaQueryListEvent) => void>()
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    writable: true,
    value: (query: string) => ({
      matches: query.includes('dark') ? dark : false,
      media: query,
      onchange: null,
      addEventListener: (_: string, l: (e: MediaQueryListEvent) => void) => listeners.add(l),
      removeEventListener: (_: string, l: (e: MediaQueryListEvent) => void) => listeners.delete(l),
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }),
  })
  return listeners
}

/** Use English strings so assertions read naturally. */
export async function switchToEnglish(): Promise<void> {
  await i18n.changeLanguage('en')
}
