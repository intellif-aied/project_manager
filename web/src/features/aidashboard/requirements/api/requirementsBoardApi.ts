import {
  addTaskDependency,
  cancelRequirement,
  createRequirement,
  createTask,
  deleteRequirement,
  deleteTask,
  fetchAllSessionTokens,
  fetchFollows,
  fetchRequirement,
  fetchRequirements,
  fetchTask,
  fetchTasks,
  fetchTaskAssignees,
  fetchTeams,
  followTarget,
  regenerateAC,
  removeTaskDependency,
  restoreRequirement,
  unfollowTarget,
  updateRequirement,
  updateSessionRequirement,
  updateSessionTask,
  updateTask,
  updateTaskProgress,
  updateTaskStatus
} from "../../api/client";
import type { Requirement, Task } from "../../api/types";
import type {
  CreateMockRequirementInput,
  CreateMockTaskInput,
  FavoriteTargetType,
  MockFavorite,
  MockRequirement,
  MockTask,
  MockTaskStatus,
  MockTokenSource,
  RequirementStage,
  UpdateBoardRequirementInput,
  UpdateBoardTaskInput
} from "../types";

function normalizeRequirement(requirement: Requirement): MockRequirement {
  const owners =
    requirement.owners && requirement.owners.length
      ? requirement.owners
      : requirement.owner_id
        ? [
            {
              id: requirement.owner_id,
              name: requirement.owner_name ?? requirement.owner_id,
              role: "",
              team_id: requirement.owner_team_id,
              team_name: requirement.owner_team_name
            }
          ]
        : [];
  const ownerIds =
    requirement.owner_ids && requirement.owner_ids.length
      ? requirement.owner_ids
      : owners.map((owner) => owner.id);
  return {
    id: requirement.id,
    title: requirement.title,
    description: requirement.description,
    feishu_doc_url: requirement.feishu_doc_url,
    acceptance_criteria: requirement.acceptance_criteria ?? [],
    creator_id: requirement.creator_id,
    creator_name: requirement.creator_name,
    creator_role: requirement.creator_role,
    owner_ids: ownerIds,
    owners,
    owner_id: requirement.owner_id,
    owner_name: requirement.owner_name,
    owner_team_id: requirement.owner_team_id,
    owner_team_name: requirement.owner_team_name,
    status: requirement.status,
    priority: requirement.priority,
    progress: requirement.progress ?? 0,
    deadline: requirement.deadline,
    team_ids: requirement.team_ids ?? [],
    team_names: requirement.team_names ?? [],
    token_source_ids: requirement.token_source_ids ?? [],
    follow_summary: requirement.follow_summary ?? {
      count: 0,
      score: 0,
      level: "normal"
    },
    risk_summary: requirement.risk_summary ?? {
      blocked: 0,
      overdue: 0,
      requirement_overdue: 0,
      dependency_conflict: 0
    },
    can_update: requirement.can_update,
    can_change_status: requirement.can_change_status,
    can_cancel: requirement.can_cancel,
    can_restore: requirement.can_restore,
    can_delete: requirement.can_delete,
    can_manage_ac: requirement.can_manage_ac,
    can_create_task: requirement.can_create_task,
    created_at: requirement.created_at,
    updated_at: requirement.updated_at,
    version: requirement.version
  };
}

function normalizeTask(task: Task): MockTask {
  return {
    id: task.id,
    requirement_id: task.requirement_id,
    requirement_title: task.requirement_title ?? "",
    title: task.title,
    creator_tl_id: task.creator_tl_id,
    acceptance_criteria: task.acceptance_criteria ?? [],
    assignee_id: task.assignee_id,
    assignee_name: task.assignee_name,
    status: (task.display_status || task.status) as MockTaskStatus,
    priority: task.priority,
    progress: task.progress ?? 0,
    due_date: task.due_date,
    dependencies: task.dependencies ?? [],
    blocking: task.blocking ?? [],
    risk_types: task.risk_types ?? [],
    token_source_ids: task.token_source_ids ?? [],
    follow_summary: task.follow_summary ?? {
      count: 0,
      score: 0,
      level: "normal"
    },
    can_update_meta: task.can_update_meta,
    can_reassign: task.can_reassign,
    can_update_status: task.can_update_status,
    can_update_progress: task.can_update_progress,
    can_manage_dependencies: task.can_manage_dependencies,
    can_delete: task.can_delete,
    created_at: task.created_at,
    updated_at: task.updated_at,
    version: task.version
  };
}

function dateDaysAgo(days: number) {
  const date = new Date();
  date.setUTCDate(date.getUTCDate() - days);
  return date.toISOString().slice(0, 10);
}

async function getNormalizedTask(taskId: string) {
  return normalizeTask(await fetchTask(taskId));
}

export const requirementsBoardApi = {
  async listRequirements() {
    return (await fetchRequirements()).map(normalizeRequirement);
  },

  async getRequirement(id: string) {
    return normalizeRequirement(await fetchRequirement(id));
  },

  async createRequirement(input: CreateMockRequirementInput) {
    return normalizeRequirement(await createRequirement(input));
  },

  async updateRequirement(id: string, input: UpdateBoardRequirementInput) {
    return normalizeRequirement(await updateRequirement(id, input as unknown as Record<string, unknown>));
  },

  async updateRequirementStage(id: string, status: RequirementStage, baseVersion: number) {
    return normalizeRequirement(await updateRequirement(id, { status, base_version: baseVersion }));
  },

  async cancelRequirement(id: string, baseVersion: number) {
    return normalizeRequirement(await cancelRequirement(id, baseVersion));
  },

  async restoreRequirement(id: string, baseVersion: number) {
    return normalizeRequirement(await restoreRequirement(id, baseVersion));
  },

  async deleteRequirement(id: string, baseVersion: number) {
    return deleteRequirement(id, baseVersion);
  },

  async regenerateAC(id: string, baseVersion: number) {
    return normalizeRequirement(await regenerateAC(id, baseVersion));
  },

  async listTasks(requirementId?: string) {
    return (
      await fetchTasks({
        scope: "requirements",
        ...(requirementId ? { requirement_id: requirementId } : {})
      })
    ).map(normalizeTask);
  },

  async getTask(id: string) {
    return getNormalizedTask(id);
  },

  async createTask(input: CreateMockTaskInput) {
    const created = await createTask({
      requirement_id: input.requirement_id,
      title: input.title,
      acceptance_criteria: input.acceptance_criteria ?? [],
      assignee_id: input.assignee_id,
      priority: input.priority,
      due_date: input.due_date,
      depends_on_ids: input.dependency_task_ids
    });
    return getNormalizedTask(created.id);
  },

  async updateTask(id: string, input: UpdateBoardTaskInput) {
    return normalizeTask(await updateTask(id, input as unknown as Record<string, unknown>));
  },

  async deleteTask(id: string, baseVersion: number) {
    return deleteTask(id, baseVersion);
  },

  async updateTaskProgress(taskId: string, progress: number, baseVersion: number) {
    return normalizeTask(await updateTaskProgress(taskId, progress, baseVersion));
  },

  async updateTaskStatus(taskId: string, status: Exclude<MockTaskStatus, "blocked">, baseVersion: number) {
    return normalizeTask(await updateTaskStatus(taskId, status, baseVersion));
  },

  async addTaskDependency(taskId: string, dependsOnId: string, baseVersion: number) {
    return normalizeTask(await addTaskDependency(taskId, dependsOnId, baseVersion));
  },

  async removeTaskDependency(taskId: string, dependsOnId: string, baseVersion: number) {
    return normalizeTask(await removeTaskDependency(taskId, dependsOnId, baseVersion));
  },

  async listTeams() {
    return fetchTeams();
  },

  async listAssignees() {
    const users = await fetchTaskAssignees();
    return users.map((user) => ({
        id: user.id,
        name: user.name,
        employee_id: user.employee_id,
        team_id: user.team_id ?? ""
      }));
  },

  async listFavorites(): Promise<MockFavorite[]> {
    return (await fetchFollows()).map((follow) => ({
      user_id: follow.user_id,
      target_type: follow.target_type,
      target_id: follow.target_id,
      created_at: follow.created_at
    }));
  },

  async toggleFavorite(targetType: FavoriteTargetType, targetId: string) {
    const follows = await fetchFollows();
    const followed = follows.some(
      (item) => item.target_type === targetType && item.target_id === targetId
    );
    return followed
      ? unfollowTarget(targetType, targetId)
      : followTarget(targetType, targetId);
  },

  async listTokenSources(): Promise<MockTokenSource[]> {
    try {
      const sources = await fetchAllSessionTokens({
        from: dateDaysAgo(90),
        to: new Date().toISOString().slice(0, 10),
        scope: "mine"
      });
      return sources.map((source) => ({
        id: source.session_id,
        recorded_at: source.started_at,
        tool: source.agent_type,
        uploader: source.user_name,
        token: source.total_tokens,
        summary: source.session_ref
      }));
    } catch {
      return [];
    }
  },

  async linkTaskTokenSources(taskId: string, sourceIds: string[]) {
    await Promise.all(sourceIds.map((sourceId) => updateSessionTask(sourceId, taskId)));
    return getNormalizedTask(taskId);
  },

  async unlinkTaskTokenSource(taskId: string, sourceId: string) {
    await updateSessionTask(sourceId, null);
    return getNormalizedTask(taskId);
  },

  async linkRequirementTokenSources(requirementId: string, sourceIds: string[]) {
    await Promise.all(
      sourceIds.map((sourceId) => updateSessionRequirement(sourceId, requirementId))
    );
    return normalizeRequirement(await fetchRequirement(requirementId));
  },

  async unlinkRequirementTokenSource(requirementId: string, sourceId: string) {
    await updateSessionRequirement(sourceId, null);
    return normalizeRequirement(await fetchRequirement(requirementId));
  }
};
