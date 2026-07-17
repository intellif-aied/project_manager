import { MenuOutlined, QuestionCircleOutlined, RocketOutlined } from "@ant-design/icons";
import { Button, Popover } from "antd";
import { useEffect, useState } from "react";
import { useLocation } from "react-router-dom";

import { HelpCenter } from "@/layouts/HelpCenter/HelpCenter";
import { isBusinessRole } from "@/layouts/HelpCenter/helpCenterContent";
import { appRoutes } from "@/router/routes";
import { findBestMenuMatch, findRouteByPath } from "@/router/routeAccess";
import { UserMenu } from "@/shared/auth/UserMenu";
import { useAuth } from "@/shared/auth/authContext";
import { Nav } from "@/shared/components/Nav/Nav";
import { useLayoutStore } from "@/stores/layoutStore";

import { useHeaderNav } from "./headerNavContext";
import "./Header.css";

const HELP_DISCOVERY_STORAGE_PREFIX = "aida:help-center-discovery:v1";
const HELP_DISCOVERY_DELAY_MS = 900;
const helpDiscoverySeenInMemory = new Set<string>();

function helpDiscoveryStorageKey(userId: string) {
  return `${HELP_DISCOVERY_STORAGE_PREFIX}:${userId}`;
}

function hasSeenHelpDiscovery(userId: string) {
  const storageKey = helpDiscoveryStorageKey(userId);
  if (helpDiscoverySeenInMemory.has(storageKey)) return true;
  try {
    return window.localStorage.getItem(storageKey) === "seen";
  } catch {
    return false;
  }
}

function markHelpDiscoverySeen(userId: string) {
  const storageKey = helpDiscoveryStorageKey(userId);
  helpDiscoverySeenInMemory.add(storageKey);
  try {
    window.localStorage.setItem(storageKey, "seen");
  } catch {
    // The in-memory marker still prevents repeated prompts in this app session.
  }
}

export function Header() {
  const location = useLocation();
  const { user } = useAuth();
  const [helpOpen, setHelpOpen] = useState(false);
  const [helpDiscoveryOpen, setHelpDiscoveryOpen] = useState(false);
  const [helpDiscoveryResolvedFor, setHelpDiscoveryResolvedFor] = useState<string>();
  const { navProps } = useHeaderNav();
  const setMobileSidebarOpen = useLayoutStore((state) => state.setMobileSidebarOpen);
  const currentRoute = findRouteByPath(location.pathname, appRoutes);
  const currentMenu = findBestMenuMatch(location.pathname, appRoutes);
  const title = currentRoute?.title ?? currentMenu?.title ?? "工作台";
  const defaultBreadcrumbs =
    currentMenu?.path === "/examples/table-crud"
      ? [{ title: "Data" }, { title: currentMenu.title, path: currentMenu.path }]
      : [{ title }];
  const showHelpCenter = isBusinessRole(user?.role);
  const helpDiscoveryUserId = showHelpCenter ? user?.id : undefined;

  useEffect(() => {
    if (
      !helpDiscoveryUserId ||
      helpDiscoveryResolvedFor === helpDiscoveryUserId ||
      hasSeenHelpDiscovery(helpDiscoveryUserId)
    ) {
      return;
    }
    const timer = window.setTimeout(() => setHelpDiscoveryOpen(true), HELP_DISCOVERY_DELAY_MS);
    return () => window.clearTimeout(timer);
  }, [helpDiscoveryResolvedFor, helpDiscoveryUserId]);

  const resolveHelpDiscovery = () => {
    if (helpDiscoveryUserId) {
      markHelpDiscoverySeen(helpDiscoveryUserId);
      setHelpDiscoveryResolvedFor(helpDiscoveryUserId);
    }
    setHelpDiscoveryOpen(false);
  };

  const openHelpCenter = () => {
    resolveHelpDiscovery();
    setHelpOpen(true);
  };

  const helpDiscoveryContent = (
    <div className="app-header__help-discovery-card">
      <div className="app-header__help-discovery-eyebrow">
        <span>
          <RocketOutlined />
        </span>
        新手引导
      </div>
      <strong>第一次使用 AIDA？</strong>
      <p>从安装客户端、上传 Session 到生成第一份日报，指南会带你完整走一遍。</p>
      <div className="app-header__help-discovery-actions">
        <Button size="small" type="text" onClick={resolveHelpDiscovery}>
          稍后
        </Button>
        <Button size="small" type="primary" onClick={openHelpCenter}>
          打开使用指南
        </Button>
      </div>
    </div>
  );

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
        {showHelpCenter ? (
          <Popover
            classNames={{ root: "app-header__help-discovery" }}
            content={helpDiscoveryContent}
            destroyOnHidden
            open={helpDiscoveryOpen}
            placement="bottomRight"
            trigger={[]}
          >
            <Button
              className={`app-header__help-trigger${helpDiscoveryOpen ? " app-header__help-trigger--discovery" : ""}`}
              type="text"
              icon={<QuestionCircleOutlined />}
              onClick={openHelpCenter}
            >
              使用指南
            </Button>
          </Popover>
        ) : null}
        <UserMenu />
      </div>
      {showHelpCenter ? (
        <HelpCenter
          key={helpOpen ? "open" : "closed"}
          open={helpOpen}
          onClose={() => setHelpOpen(false)}
        />
      ) : null}
    </header>
  );
}
