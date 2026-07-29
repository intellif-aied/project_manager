import {
  BarChartOutlined,
  BellOutlined,
  DashboardOutlined,
  FileDoneOutlined,
  RobotOutlined,
  SettingOutlined,
  ProjectOutlined,
  SolutionOutlined,
  TeamOutlined
} from "@ant-design/icons";
import { AIAssetsPage } from "@/features/aidashboard/ai-assets/pages/AIAssetsPage";
import { AgentCreatePage } from "@/features/aidashboard/ai-assets/pages/AgentCreatePage";
import { AgentEditPage } from "@/features/aidashboard/ai-assets/pages/AgentEditPage";
import { AgentRunPage } from "@/features/aidashboard/ai-assets/pages/AgentRunPage";
import { AgentScheduleFormPage } from "@/features/aidashboard/ai-assets/pages/AgentScheduleFormPage";
import { MCPCreatePage } from "@/features/aidashboard/ai-assets/pages/MCPCreatePage";
import { SkillCreatePage } from "@/features/aidashboard/ai-assets/pages/SkillCreatePage";
import { DashboardPage } from "@/features/aidashboard/dashboard/DashboardPage";
import { OrganizationPage } from "@/features/aidashboard/organization/pages/OrganizationPage";
import { OrganizationUserEditPage } from "@/features/aidashboard/organization/pages/OrganizationUserEditPage";
import { ProductDocumentCreatePage } from "@/features/aidashboard/products/pages/ProductDocumentCreatePage";
import { ProductsPage } from "@/features/aidashboard/products/pages/ProductsPage";
import {
  DailyReportsPage,
  DepartmentDailyReportDetailPage,
  PersonalDailyReportDetailPage,
  ReportsPage,
  TeamDailyReportDetailPage
} from "@/features/aidashboard/reports/pages/ReportsPage";
import { WeeklyReportsPage } from "@/features/aidashboard/reports/pages/WeeklyReportsPage";
import { RequirementCreatePage } from "@/features/aidashboard/requirements/pages/RequirementCreatePage";
import { RequirementDetailPage } from "@/features/aidashboard/requirements/pages/RequirementDetailPage";
import { RequirementsListPage } from "@/features/aidashboard/requirements/pages/RequirementsListPage";
import { SessionsPage } from "@/features/aidashboard/sessions/pages/SessionsPage";
import { TaskCreatePage } from "@/features/aidashboard/tasks/pages/TaskCreatePage";
import { TaskDetailPage } from "@/features/aidashboard/tasks/pages/TaskDetailPage";
import { TasksListPage } from "@/features/aidashboard/tasks/pages/TasksListPage";
import { TokensPage } from "@/features/aidashboard/tokens/pages/TokensPage";
import { TokenAnalyticsManagementPage } from "@/features/aidashboard/tokens/pages/TokenAnalyticsPage";
import { TokenMemberAnalyticsPage } from "@/features/aidashboard/tokens/pages/TokenMemberAnalyticsPage";
import { PricingManagementPage } from "@/features/aidashboard/tokens/pages/PricingManagementPage";

import { PagePlaceholder } from "./PagePlaceholder";
import type { AppRoute } from "./types";

export const appRoutes: AppRoute[] = [
  {
    path: "/dashboard",
    title: "工作台",
    icon: <DashboardOutlined />,
    menuGroup: "工作空间",
    menuOrder: 10,
    element: <DashboardPage />
  },
  {
    path: "/organization",
    title: "组织成员",
    icon: <TeamOutlined />,
    menuGroup: "工作空间",
    menuOrder: 90,
    roles: ["admin"],
    element: <OrganizationPage />
  },
  {
    path: "/organization/users/:id/edit",
    title: "编辑成员",
    hideInMenu: true,
    roles: ["admin"],
    element: <OrganizationUserEditPage />
  },
  {
    path: "/requirements",
    title: "需求看板",
    icon: <ProjectOutlined />,
    menuGroup: "工作空间",
    menuOrder: 20,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <RequirementsListPage />
  },
  {
    path: "/requirements/create",
    title: "新建需求",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader"],
    element: <RequirementCreatePage />
  },
  {
    path: "/requirements/:id",
    title: "需求详情",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <RequirementDetailPage />
  },
  {
    path: "/tasks",
    title: "任务",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <TasksListPage />
  },
  {
    path: "/tasks/create",
    title: "创建任务",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <TaskCreatePage />
  },
  {
    path: "/tasks/:id",
    title: "任务详情",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <TaskDetailPage />
  },
  {
    path: "/products",
    title: "我的工作",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <ProductsPage />
  },
  {
    path: "/products/documents/create",
    title: "添加文档",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <ProductDocumentCreatePage />
  },
  {
    path: "/sessions",
    title: "AI 工作记录",
    icon: <SolutionOutlined />,
    menuGroup: "业务",
    menuOrder: 60,
    hideInMenu: true,
    element: <SessionsPage />
  },
  {
    path: "/reports",
    title: "报告",
    icon: <FileDoneOutlined />,
    menuGroup: "工作空间",
    menuOrder: 30,
    element: <ReportsPage />
  },
  {
    path: "/reports/daily",
    title: "日报",
    hideInMenu: true,
    element: <DailyReportsPage />
  },
  {
    path: "/reports/daily/personal/:id",
    title: "个人日报详情",
    hideInMenu: true,
    element: <PersonalDailyReportDetailPage />
  },
  {
    path: "/reports/daily/team/:id",
    title: "小组日报详情",
    hideInMenu: true,
    element: <TeamDailyReportDetailPage />
  },
  {
    path: "/reports/daily/department/:id",
    title: "部门日报详情",
    hideInMenu: true,
    element: <DepartmentDailyReportDetailPage />
  },
  {
    path: "/reports/weekly",
    title: "周报",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <WeeklyReportsPage />
  },
  {
    path: "/tokens",
    title: "我的 Token",
    icon: <BarChartOutlined />,
    menuGroup: "AI 管理",
    menuOrder: 60,
    element: <TokensPage />
  },
  {
    path: "/token-analytics",
    title: "团队 AI 使用分析",
    icon: <BarChartOutlined />,
    menuGroup: "AI 管理",
    menuOrder: 61,
    roles: ["admin", "director", "team_leader"],
    feature: "token_analytics",
    element: <TokenAnalyticsManagementPage />
  },
  {
    path: "/token-analytics/member/:userID",
    title: "成员 Token 详情",
    hideInMenu: true,
    roles: ["admin", "director", "team_leader"],
    feature: "token_analytics",
    element: <TokenMemberAnalyticsPage />
  },
  {
    path: "/admin/pricing",
    title: "价格管理",
    icon: <SettingOutlined />,
    menuGroup: "AI 管理",
    menuOrder: 62,
    roles: ["admin"],
    feature: "pricing_management",
    element: <PricingManagementPage />
  },
  {
    path: "/ai-assets",
    title: "AI 资产",
    icon: <RobotOutlined />,
    menuGroup: "AI 管理",
    menuOrder: 50,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <AIAssetsPage />
  },
  {
    path: "/ai-assets/skills/new",
    title: "新建 Skill",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <SkillCreatePage />
  },
  {
    path: "/ai-assets/agents/new",
    title: "新建 Managed Agent",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <AgentCreatePage />
  },
  {
    path: "/ai-assets/agents/:agentId/edit",
    title: "编辑 Managed Agent",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <AgentEditPage />
  },
  {
    path: "/ai-assets/agents/:agentId/run",
    title: "运行 Managed Agent",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <AgentRunPage />
  },
  {
    path: "/ai-assets/agent-schedules/new",
    title: "新建定时报告任务",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <AgentScheduleFormPage />
  },
  {
    path: "/ai-assets/agent-schedules/:scheduleId/edit",
    title: "编辑定时报告任务",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <AgentScheduleFormPage />
  },
  {
    path: "/ai-assets/mcp/new",
    title: "新建 MCP Server",
    hideInMenu: true,
    roles: ["admin", "director", "pm", "team_leader", "employee"],
    element: <MCPCreatePage />
  },
  {
    path: "/notifications",
    title: "空闲告警",
    icon: <BellOutlined />,
    hideInMenu: true,
    element: <PagePlaceholder title="空闲告警" />
  }
];
