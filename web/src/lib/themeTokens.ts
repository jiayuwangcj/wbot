import type { ThemeConfig } from "antd";

function cssToken(name: string): string {
  if (typeof document === "undefined") return `var(${name})`;
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim() || `var(${name})`;
}

export function getAntdTokens(): NonNullable<ThemeConfig["token"]> {
  return {
    colorPrimary: cssToken("--accent"),
    colorBgBase: cssToken("--bg"),
    colorBgContainer: cssToken("--surface"),
    colorText: cssToken("--fg"),
    colorTextSecondary: cssToken("--muted"),
    colorBorder: cssToken("--border"),
    borderRadius: 8,
    controlHeight: 40,
  };
}

export function cssColor(name: string): string {
  return cssToken(name);
}
