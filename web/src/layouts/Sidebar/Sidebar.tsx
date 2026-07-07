import { LeftOutlined, RightOutlined } from "@ant-design/icons";
import { Layout, Menu } from "antd";
import type { MenuProps } from "antd";
import { useMemo } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { getMenuRoutesForUser } from "@/router/menu";
import { appRoutes } from "@/router/routes";
import { findBestMenuMatch } from "@/router/routeAccess";
import type { AppRoute } from "@/router/types";
import { useAuth } from "@/shared/auth/authContext";
import { useLayoutStore } from "@/stores/layoutStore";

import "./Sidebar.css";

interface SidebarContentProps {
  collapsed?: boolean;
  onNavigate?: () => void;
}

export function SidebarContent({ collapsed = false, onNavigate }: SidebarContentProps) {
  const navigate = useNavigate();
  const location = useLocation();
  const { user } = useAuth();

  const items = useMemo<MenuProps["items"]>(() => {
    const routes = getMenuRoutesForUser(appRoutes, user);
    type MenuItem = NonNullable<MenuProps["items"]>[number];
    type MenuGroup = {
      key: string;
      label: string;
      type: "group";
      children: MenuItem[];
      order: number;
    };

    const toMenuItem = (route: AppRoute): MenuItem => {
      const childItems = route.children?.map(toMenuItem).filter(Boolean);

      return {
        key: route.path,
        icon: route.icon,
        label: route.title,
        children: childItems && childItems.length > 0 ? childItems : undefined
      };
    };

    const groups = new Map<string, MenuGroup>();

    routes
      .slice()
      .sort(
        (a, b) =>
          (a.menuOrder ?? Number.MAX_SAFE_INTEGER) - (b.menuOrder ?? Number.MAX_SAFE_INTEGER)
      )
      .forEach((route, index) => {
        const groupLabel = route.menuGroup ?? "应用导航";
        const group = groups.get(groupLabel) ?? {
          key: groupLabel,
          label: groupLabel,
          type: "group" as const,
          children: [],
          order: route.menuOrder ?? index
        };

        group.children.push(toMenuItem(route));
        group.order = Math.min(group.order, route.menuOrder ?? index);
        groups.set(groupLabel, group);
      });

    return Array.from(groups.values())
      .sort((a, b) => a.order - b.order)
      .map((group) => ({
        key: group.key,
        label: group.label,
        type: group.type,
        children: group.children
      }))
      .filter((group) => group.children.length > 0);
  }, [user]);

  const selectedKey = findBestMenuMatch(location.pathname, appRoutes)?.path;

  return (
    <>
      <a className="app-sidebar__brand" href="/" aria-label="AIDashboard 首页">
        <img className="app-sidebar__brand-logo" src="/brand-logo.png" alt="" aria-hidden="true" />
        {!collapsed && (
          <span className="app-sidebar__brand-copy">
            <strong>AIDashboard</strong>
            <small>AIDA OPS</small>
          </span>
        )}
      </a>
      <Menu
        mode="inline"
        theme="light"
        selectedKeys={selectedKey ? [selectedKey] : []}
        items={items}
        onClick={({ key }) => {
          navigate(key);
          onNavigate?.();
        }}
      />
    </>
  );
}

export function Sidebar() {
  const collapsed = useLayoutStore((state) => state.sidebarCollapsed);
  const setCollapsed = useLayoutStore((state) => state.setSidebarCollapsed);

  return (
    <Layout.Sider
      className="app-sidebar"
      width={216}
      collapsedWidth={64}
      collapsed={collapsed}
      trigger={null}
    >
      <SidebarContent collapsed={collapsed} />
      <button
        type="button"
        className="app-sidebar__collapse-trigger"
        aria-label={collapsed ? "展开侧边栏" : "收起侧边栏"}
        onClick={() => setCollapsed(!collapsed)}
      >
        {collapsed ? <RightOutlined /> : <LeftOutlined />}
      </button>
    </Layout.Sider>
  );
}
