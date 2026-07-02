import { runtimeConfig } from "@/config/runtimeConfig";

import type { LoginCredentials, LoginResponse, User } from "./types";

export class AuthRequestError extends Error {
  status?: number;

  constructor(message: string, status?: number) {
    super(message);
    this.name = "AuthRequestError";
    this.status = status;
  }
}

function trimTrailingSlash(value: string) {
  return value.replace(/\/+$/, "");
}

function getApiUrl(baseUrl: string, path: string) {
  return `${trimTrailingSlash(baseUrl)}${path}`;
}

async function readPayload(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text.trim()) return null;
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return text;
  }
}

function getRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" ? (value as Record<string, unknown>) : null;
}

function pickString(value: unknown, keys: string[]) {
  const record = getRecord(value);
  if (!record) return null;
  for (const key of keys) {
    const candidate = record[key];
    if (typeof candidate === "string" && candidate.trim()) {
      return candidate.trim();
    }
    if (typeof candidate === "number" && Number.isFinite(candidate)) {
      return String(candidate);
    }
  }
  return null;
}

function getErrorMessage(payload: unknown, fallback: string) {
  if (typeof payload === "string" && payload.trim()) return payload.trim();
  return pickString(payload, ["message", "msg", "error"]) ?? fallback;
}

function normalizeLoginErrorMessage(message: string, status?: number) {
  const normalized = message.trim().toLowerCase();

  if (normalized.includes("username and password are required")) {
    return "请输入账号和密码。";
  }
  if (normalized.includes("invalid request body")) {
    return "登录请求格式不正确，请刷新页面后重试。";
  }
  if (normalized.includes("invalid username or password")) {
    return "账号或密码错误，请重新输入。";
  }
  if (normalized.includes("aihub host is not configured")) {
    return "登录服务未配置，请联系管理员处理。";
  }
  if (normalized.includes("aihub token missing uid")) {
    return "登录服务返回异常，请稍后重试。";
  }
  if (normalized.includes("aida access is not enabled")) {
    return "当前账号尚未开通 Aida 访问权限，请联系管理员开通。";
  }
  if (normalized.includes("aida access is disabled")) {
    return "当前账号已被停用，请联系管理员处理。";
  }
  if (normalized.includes("failed") || normalized.includes("unreachable") || normalized.includes("timeout")) {
    return "登录服务暂时不可用，请稍后重试。";
  }
  if (status === 401) {
    return "账号或密码错误，请重新输入。";
  }
  if (status === 403) {
    return "当前账号无权登录 Aida，请联系管理员处理。";
  }
  if (status === 500 || status === 502 || status === 503 || status === 504) {
    return "登录服务暂时不可用，请稍后重试。";
  }
  return "登录失败，请稍后重试。";
}

function normalizeCurrentUserErrorMessage(message: string, status?: number) {
  const normalized = message.trim().toLowerCase();
  if (normalized.includes("not authenticated") || status === 401) {
    return "登录状态已失效，请重新登录。";
  }
  if (status === 403) {
    return "当前账号无权访问该页面，请联系管理员处理。";
  }
  if (status === 500 || status === 502 || status === 503 || status === 504) {
    return "用户信息加载失败，请稍后重试。";
  }
  return "用户信息加载失败，请稍后重试。";
}

function resolveUser(payload: unknown): User {
  const record = getRecord(payload);
  if (!record) {
    throw new Error("登录响应格式无效");
  }
  const id = pickString(record, ["id"]);
  if (!id) {
    throw new Error("登录响应缺少用户 ID");
  }
  const username = pickString(record, ["username", "employee_id"]) ?? "";
  const nickname = pickString(record, ["nickname", "name"]) ?? "";
  const appRole = (pickString(record, ["app_role", "role"]) ?? "employee") as User["role"];
  return {
    id,
    username,
    nickname,
    employee_id: username,
    email: pickString(record, ["email"]) ?? "",
    name: nickname || username,
    app_role: appRole,
    role: appRole,
    team_id: pickString(record, ["team_id"]) ?? null,
    team_name: pickString(record, ["team_name"]) ?? null,
    local_enabled: getRecord(record)?.local_enabled === false ? false : true,
    status: (pickString(record, ["status"]) ?? "active") as User["status"],
    deactivated_at: pickString(record, ["deactivated_at"]) ?? null,
    last_synced_at: pickString(record, ["last_synced_at"]) ?? null,
    created_at: pickString(record, ["created_at"]) ?? undefined
  };
}

function resolveLoginResponse(payload: unknown): LoginResponse {
  const record = getRecord(payload) ?? {};
  const recordData = getRecord(record.data);
  const token = pickString(record, ["access_token", "token"]) ?? pickString(recordData, ["access_token", "token"]);
  if (!token) {
    throw new Error("登录响应缺少 Token");
  }
  const userRecordFromTop = getRecord(record.user);
  const userRecordFromData = getRecord(recordData?.user);
  const userPayload: unknown = userRecordFromTop ?? userRecordFromData ?? recordData ?? record;
  return { token, user: resolveUser(userPayload) };
}

export async function loginWithPassword(credentials: LoginCredentials): Promise<LoginResponse> {
  const response = await fetch(getApiUrl(runtimeConfig.authApiBaseUrl, "/auth/login"), {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(credentials)
  });
  const payload = await readPayload(response);
  if (!response.ok) {
    const rawMessage = getErrorMessage(payload, "登录失败，请检查工号或密码");
    throw new AuthRequestError(
      normalizeLoginErrorMessage(rawMessage, response.status),
      response.status
    );
  }
  return resolveLoginResponse(payload);
}

export async function fetchCurrentUser(token: string, signal?: AbortSignal): Promise<User> {
  const response = await fetch(getApiUrl(runtimeConfig.userApiBaseUrl, "/auth/me"), {
    headers: {
      Accept: "application/json",
      Authorization: `Bearer ${token}`
    },
    signal
  });
  const payload = await readPayload(response);
  if (!response.ok) {
    const rawMessage = getErrorMessage(payload, "当前用户加载失败");
    throw new AuthRequestError(normalizeCurrentUserErrorMessage(rawMessage, response.status), response.status);
  }
  return resolveUser(payload);
}
