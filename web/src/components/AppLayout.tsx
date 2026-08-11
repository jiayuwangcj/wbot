import type { ReactNode } from "react";
import { ConfigProvider, Layout, Button, Tooltip } from "antd";
import { MoonOutlined, SunOutlined, BgColorsOutlined } from "@ant-design/icons";
import { isNavActive, NAV_ITEMS } from "../lib/nav";
import { useTheme } from "../hooks/useTheme";

import "../styles/app.css";

export interface AppLayoutProps {
  children: ReactNode;
}

function ThemeIcon({ mode }: { mode: ReturnType<typeof useTheme>["theme"] }): ReactNode {
  if (mode === "light") return <MoonOutlined />;
  if (mode === "dark") return <BgColorsOutlined />;
  return <SunOutlined />;
}

export function AppLayout({ children }: AppLayoutProps): ReactNode {
  const themeState = useTheme();
  const currentPath = window.location.pathname;
  const label = themeState.theme === "light" ? "切换到深色主题" : themeState.theme === "dark" ? "切换到高对比深色主题" : "切换到浅色主题";
  return (
    <ConfigProvider theme={themeState.antdConfig}>
      <Layout className="app-layout">
        <Layout.Header className="app-header">
          <a className="app-brand" href="/ui/">wbot</a>
          <nav className="app-nav" aria-label="主导航">
            {NAV_ITEMS.map((item) => (
              <a className={`app-nav-link${isNavActive(item, currentPath) ? " active" : ""}`} href={item.href} key={item.href}>
                {item.label}
              </a>
            ))}
          </nav>
          <Tooltip title={label}>
            <Button id="theme-toggle" aria-label={label} onClick={themeState.toggleTheme} type="text" icon={<ThemeIcon mode={themeState.theme} />} />
          </Tooltip>
        </Layout.Header>
        <Layout.Content className="app-content">{children}</Layout.Content>
      </Layout>
    </ConfigProvider>
  );
}
