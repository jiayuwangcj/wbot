import "@testing-library/jest-dom/vitest";

if (!window.matchMedia) {
  window.matchMedia = (query: string): MediaQueryList => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => undefined,
    removeListener: () => undefined,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    dispatchEvent: () => false,
  });
}

const nativeGetComputedStyle = window.getComputedStyle.bind(window);
window.getComputedStyle = ((element: Element, pseudoElement?: string | null): CSSStyleDeclaration => {
  if (pseudoElement) return nativeGetComputedStyle(element);
  try {
    return nativeGetComputedStyle(element, pseudoElement);
  } catch {
    return nativeGetComputedStyle(element);
  }
}) as typeof window.getComputedStyle;
