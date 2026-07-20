import { useState } from "react";
import { Navigate, useParams, useSearchParams } from "react-router-dom";

import { businessDateKey } from "@/shared/utils/businessTime";

import {
  getTokenAnalyticsPresetRange,
  isTokenAnalyticsDateRange
} from "../tokenAnalyticsView";
import type { TokenAnalyticsDateRange } from "../tokenAnalyticsView";
import { TokenAnalyticsPage } from "./TokenAnalyticsPage";

function memberDateRange(searchParams: URLSearchParams): TokenAnalyticsDateRange {
  const from = searchParams.get("from")?.trim() ?? "";
  const to = searchParams.get("to")?.trim() ?? "";
  if (isTokenAnalyticsDateRange({ from, to })) return { from, to };
  return getTokenAnalyticsPresetRange("today", businessDateKey());
}

export function TokenMemberAnalyticsPage() {
  const { userID } = useParams<{ userID: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const [dateRange, setDateRange] = useState<TokenAnalyticsDateRange>(() =>
    memberDateRange(searchParams)
  );

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
      dateRange={dateRange}
      onDateRangeChange={(nextRange) => {
        setDateRange(nextRange);
        const nextSearchParams = new URLSearchParams(searchParams);
        nextSearchParams.set("from", nextRange.from);
        nextSearchParams.set("to", nextRange.to);
        setSearchParams(nextSearchParams, { replace: true });
      }}
    />
  );
}
