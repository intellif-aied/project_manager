import { Navigate, useLocation, useParams, useSearchParams } from "react-router-dom";

import { appendSearch } from "@/shared/utils/urlQuery";

export function RequirementDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const location = useLocation();
  const [searchParams] = useSearchParams();
  const taskId = searchParams.get("taskId");
  const next = new URLSearchParams(location.search);

  next.delete("taskId");
  next.delete("focus");

  if (taskId) {
    return <Navigate replace to={appendSearch(`/tasks/${taskId}`, next)} />;
  }

  if (id) {
    next.set("requirementId", id);
  }

  return <Navigate replace to={appendSearch("/requirements", next)} />;
}
