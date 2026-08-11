import { useEffect, useRef } from "react";

export const AUTO_REFRESH_MS = 30_000;

export function useAutoRefresh(refresh: () => void | Promise<void>, interval = AUTO_REFRESH_MS, enabled = true): void {
  const refreshRef = useRef(refresh);
  refreshRef.current = refresh;

  useEffect(() => {
    if (!enabled) return undefined;
    let timer: number | null = null;
    const clearTimer = (): void => {
      if (timer !== null) window.clearInterval(timer);
      timer = null;
    };
    const startTimer = (): void => {
      clearTimer();
      if (document.visibilityState === "visible") {
        timer = window.setInterval(() => void refreshRef.current(), interval);
      }
    };
    const onVisibilityChange = (): void => {
      if (document.visibilityState === "hidden") clearTimer();
      else {
        void refreshRef.current();
        startTimer();
      }
    };
    startTimer();
    document.addEventListener("visibilitychange", onVisibilityChange);
    return () => {
      clearTimer();
      document.removeEventListener("visibilitychange", onVisibilityChange);
    };
  }, [enabled, interval]);
}
