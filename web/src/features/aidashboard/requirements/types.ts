// Requirements board view-model types — aliased to real backend DTOs.
// These names are kept for incremental migration off the legacy "Mock*" names.
import type {
  FollowTargetType,
  Requirement,
  RequirementPriority as ApiRequirementPriority,
  RequirementFollowSummaryDTO,
  ResponsibleUserDTO,
  RequirementRiskSummaryDTO,
  RequirementStatus,
  Task,
  TaskDep,
  TaskPriority,
  TaskStatus
} from "../api/types";

export type RequirementStage = RequirementStatus;
export type RequirementPriority = ApiRequirementPriority;
export type BoardTaskStatus = TaskStatus;
export type BoardTaskPriority = TaskPriority;

export type BoardTeam = { id: string; name: string };
export type BoardAssignee = {
  id: string;
  name: string;
  employee_id: string;
  team_id: string;
};
export type BoardTaskDependency = TaskDep;

export interface BoardTokenSource {
  id: string;
  recorded_at: string;
  tool: string;
  uploader: string;
  token: number;
  summary: string;
}

export interface BoardRequirement {
  id: string;
  title: string;
  description: string;
  feishu_doc_url?: string;
  acceptance_criteria: string[];
  creator_id: string;
  creator_name: string;
  creator_role: string;
  responsible_user_ids: string[];
  responsible_users: ResponsibleUserDTO[];
  status: RequirementStage;
  priority: RequirementPriority;
  progress: number;
  deadline?: string;
  team_ids: string[];
  team_names: string[];
  token_source_ids: string[];
  dependencies: BoardTaskDependency[];
  blocking: BoardTaskDependency[];
  follow_summary: RequirementFollowSummaryDTO;
  risk_summary?: RequirementRiskSummaryDTO;
  can_update?: boolean;
  can_change_status?: boolean;
  can_cancel?: boolean;
  can_restore?: boolean;
  can_delete?: boolean;
  can_manage_ac?: boolean;
  can_create_task?: boolean;
  created_at: string;
  updated_at: string;
  version: number;
}

export interface BoardTask {
  id: string;
  requirement_id: string;
  requirement_title: string;
  title: string;
  creator_id: string;
  creator_name: string;
  responsible_user_ids: string[];
  responsible_users: ResponsibleUserDTO[];
  acceptance_criteria: string[];
  status: BoardTaskStatus;
  priority: BoardTaskPriority;
  progress: number;
  due_date?: string;
  dependencies: BoardTaskDependency[];
  blocking: BoardTaskDependency[];
  token_source_ids: string[];
  follow_summary: RequirementFollowSummaryDTO;
  risk_types?: Array<"blocked" | "overdue" | "dependency_conflict">;
  can_update_meta?: boolean;
  can_reassign?: boolean;
  can_update_status?: boolean;
  can_update_progress?: boolean;
  can_manage_dependencies?: boolean;
  can_delete?: boolean;
  created_at: string;
  updated_at: string;
  version: number;
}

export interface CreateBoardRequirementInput {
  title: string;
  description: string;
  priority: RequirementPriority;
  deadline?: string;
  responsible_user_ids?: string[];
  team_ids?: string[];
  feishu_doc_url?: string;
  acceptance_criteria: string[];
}

export interface UpdateBoardRequirementInput {
  title?: string;
  description?: string;
  priority?: RequirementPriority;
  status?: RequirementStage;
  deadline?: string;
  feishu_doc_url?: string;
  responsible_user_ids?: string[];
  team_ids?: string[];
  acceptance_criteria?: string[];
  base_version: number;
}

export interface CreateBoardTaskInput {
  requirement_id: string;
  title: string;
  acceptance_criteria: string[];
  responsible_user_ids?: string[];
  priority: BoardTaskPriority;
  due_date?: string;
  dependency_task_ids?: string[];
}

export interface UpdateBoardTaskInput {
  title?: string;
  responsible_user_ids?: string[];
  status?: BoardTaskStatus;
  priority?: BoardTaskPriority;
  progress?: number;
  due_date?: string;
  acceptance_criteria?: string[];
  base_version: number;
}

export type FavoriteTargetType = FollowTargetType;

export interface BoardFavorite {
  user_id: string;
  target_type: FavoriteTargetType;
  target_id: string;
  created_at: string;
}

// Legacy aliases — keep the old identifier names exported so callers can be
// migrated incrementally without churn. These will be removed once all call
// sites use the Board* names.
export type MockTeam = BoardTeam;
export type MockAssignee = BoardAssignee;
export type MockTaskDependency = BoardTaskDependency;
export type MockTokenSource = BoardTokenSource;
export type MockRequirement = BoardRequirement;
export type MockTask = BoardTask;
export type MockTaskStatus = BoardTaskStatus;
export type MockTaskPriority = BoardTaskPriority;
export type CreateMockRequirementInput = CreateBoardRequirementInput;
export type CreateMockTaskInput = CreateBoardTaskInput;
export type MockFavorite = BoardFavorite;

// Use type-only re-export to satisfy `isolatedModules`.
export type { Requirement, Task };
