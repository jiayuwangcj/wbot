export interface NavItem {
  label: string;
  href: string;
  path: string;
}

export const NAV_ITEMS: NavItem[] = [
  { label: "Dashboard", href: "/ui/", path: "/ui/" },
  { label: "策略", href: "/ui/watchlist.html", path: "/ui/watchlist.html" },
  { label: "回测", href: "/ui/results.html", path: "/ui/results.html" },
  { label: "数据", href: "/ui/data.html", path: "/ui/data.html" },
  { label: "Admin", href: "/ui/admin.html", path: "/ui/admin.html" },
];

export function normalizePath(pathname: string): string {
  const path = pathname.replace(/\/+$/, "") || "/";
  return path === "/ui/index.html" ? "/ui" : path;
}

export function isNavActive(item: NavItem, pathname: string): boolean {
  const current = normalizePath(pathname);
  return normalizePath(item.path) === current;
}
