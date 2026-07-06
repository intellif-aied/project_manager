import type { QueryClient } from "@tanstack/react-query";

export function invalidateRequirementTaskWorkspace(
  queryClient: QueryClient,
  options: { requirementId?: string; taskId?: string } = {}
) {
  const invalidations = [
    queryClient.invalidateQueries({
      predicate: (query) => {
        const [scope, resource] = query.queryKey;
        return scope === "requirements-board" && resource !== "task-events";
      }
    }),
    queryClient.invalidateQueries({ queryKey: ["dashboard"] }),
    queryClient.invalidateQueries({ queryKey: ["tasks"] }),
    queryClient.invalidateQueries({ queryKey: ["follows"] }),
    queryClient.invalidateQueries({ queryKey: ["sessions"] })
  ];

  if (options.requirementId) {
    invalidations.push(
      queryClient.invalidateQueries({
        queryKey: ["requirements-board", "requirement", options.requirementId]
      }),
      queryClient.invalidateQueries({
        queryKey: ["requirements-board", "tasks", options.requirementId]
      })
    );
  }

  if (options.taskId) {
    invalidations.push(
      queryClient.invalidateQueries({ queryKey: ["requirements-board", "task", options.taskId] })
    );
  }

  return Promise.all(invalidations);
}
