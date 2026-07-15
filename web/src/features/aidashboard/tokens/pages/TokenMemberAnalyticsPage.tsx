import { Navigate, useParams, useSearchParams } from "react-router-dom";

import { TokenAnalyticsPage } from "./TokenAnalyticsPage";

export function TokenMemberAnalyticsPage() {
  const { userID } = useParams<{ userID: string }>();
  const [searchParams] = useSearchParams();

  if (!userID) {
    return <Navigate to="/token-analytics" replace />;
  }

  return (
    <TokenAnalyticsPage
      scope="management"
      member={{
        id: userID,
        name: searchParams.get("name")?.trim() || "成员"
      }}
    />
  );
}
