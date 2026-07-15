import { MenuOutlined, QuestionCircleOutlined } from "@ant-design/icons";
import { Button } from "antd";
import { useState } from "react";
import { useLocation } from "react-router-dom";

import { HelpCenter } from "@/layouts/HelpCenter/HelpCenter";
import { getHelpModuleByPath } from "@/layouts/HelpCenter/helpCenterConfig";
import { appRoutes } from "@/router/routes";
import { findBestMenuMatch, findRouteByPath } from "@/router/routeAccess";
import { UserMenu } from "@/shared/auth/UserMenu";
import { useAuth } from "@/shared/auth/authContext";
import { Nav } from "@/shared/components/Nav/Nav";
import { useLayoutStore } from "@/stores/layoutStore";

import { useHeaderNav } from "./headerNavContext";
import "./Header.css";

export function Header() {
  const location = useLocation();
  const { user } = useAuth();
  const [helpOpen, setHelpOpen] = useState(false);
  const { navProps } = useHeaderNav();
  const setMobileSidebarOpen = useLayoutStore((state) => state.setMobileSidebarOpen);
  const currentRoute = findRouteByPath(location.pathname, appRoutes);
  const currentMenu = findBestMenuMatch(location.pathname, appRoutes);
  const title = currentRoute?.title ?? currentMenu?.title ?? "工作台";
  const defaultBreadcrumbs =
    currentMenu?.path === "/examples/table-crud"
      ? [{ title: "Data" }, { title: currentMenu.title, path: currentMenu.path }]
      : [{ title }];
  const helpModule = user?.role === "admin" ? undefined : getHelpModuleByPath(location.pathname);

  return (
    <header className="app-header">
      <div className="app-header__left">
        <Button
          className="app-header__menu-trigger"
          type="text"
          aria-label="打开导航"
          icon={<MenuOutlined />}
          onClick={() => setMobileSidebarOpen(true)}
        />
        <div className="app-header__context">
          <span className="app-header__eyebrow">AIDA OPS CONSOLE</span>
          <div className="app-header__page">
            <Nav
              title={navProps?.title ?? title}
              breadcrumbs={navProps?.breadcrumbs ?? defaultBreadcrumbs}
              onNavigate={navProps?.onNavigate}
              variant="breadcrumb"
            />
          </div>
        </div>
      </div>
      <div className="app-header__right">
        {helpModule ? (
          <Button
            className="app-header__help-trigger"
            type="text"
            icon={<QuestionCircleOutlined />}
            onClick={() => setHelpOpen(true)}
          >
            使用指南
          </Button>
        ) : null}
        <UserMenu />
      </div>
      {helpModule ? <HelpCenter key={helpOpen ? "open" : "closed"} open={helpOpen} onClose={() => setHelpOpen(false)} /> : null}
    </header>
  );
}
