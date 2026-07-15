export type HelpModuleKey = "quickstart" | "client" | "workspace" | "requirements" | "daily" | "weekly";

export function getHelpModuleByPath(pathname: string): HelpModuleKey | undefined {
  if (pathname.startsWith("/sessions")) return "client";
  if (pathname === "/dashboard") return "workspace";
  if (pathname.startsWith("/requirements")) return "requirements";
  if (pathname.startsWith("/tasks")) return "requirements";
  if (pathname.startsWith("/reports/daily")) return "daily";
  if (pathname.startsWith("/reports/weekly")) return "weekly";
  return undefined;
}
