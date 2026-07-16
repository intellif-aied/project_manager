import { Drawer, Layout } from "antd";
import { useLayoutEffect } from "react";
import type { PropsWithChildren } from "react";
import { useLocation } from "react-router-dom";

import { Header } from "@/layouts/Header/Header";
import { HeaderNavProvider } from "@/layouts/Header/HeaderNavProvider";
import { Sidebar, SidebarContent } from "@/layouts/Sidebar/Sidebar";
import { useLayoutStore } from "@/stores/layoutStore";
import { ReportAIRunTracker } from "@/features/aidashboard/reports/components/ReportAIRunTracker";

import "./MainLayout.css";

export function MainLayout({ children }: PropsWithChildren) {
  const location = useLocation();
  const mobileSidebarOpen = useLayoutStore((state) => state.mobileSidebarOpen);
  const setMobileSidebarOpen = useLayoutStore((state) => state.setMobileSidebarOpen);

  useLayoutEffect(() => {
    document.getElementById("main-content-scroll-container")?.scrollTo({ top: 0, left: 0 });
    setMobileSidebarOpen(false);
  }, [location.pathname, setMobileSidebarOpen]);

  return (
    <HeaderNavProvider>
      <ReportAIRunTracker />
      <Layout className="main-layout">
        <Sidebar />
        <Drawer
          className="main-layout__mobile-drawer"
          size={280}
          placement="left"
          open={mobileSidebarOpen}
          onClose={() => setMobileSidebarOpen(false)}
          closeIcon={null}
          title={null}
          styles={{ body: { padding: 0 }, header: { display: "none" } }}
        >
          <nav className="app-sidebar app-sidebar--mobile" aria-label="移动端导航">
            <SidebarContent onNavigate={() => setMobileSidebarOpen(false)} />
          </nav>
        </Drawer>
        <Layout className="main-layout__workspace">
          <Header />
          <Layout.Content className="main-layout__content" id="main-content-scroll-container">
            <div className="main-layout__inner">{children}</div>
          </Layout.Content>
        </Layout>
      </Layout>
    </HeaderNavProvider>
  );
}
