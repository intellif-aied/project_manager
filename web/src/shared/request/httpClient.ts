import axios from "axios";
import type { AxiosError, AxiosRequestConfig } from "axios";

import { runtimeConfig } from "@/config/runtimeConfig";
import { clearAuthSession, getAuthSession } from "@/shared/auth/session";
import { feedback } from "@/shared/feedback/feedback";

import { HttpError } from "./types";
import type { ApiResponse, RequestOptions } from "./types";

const REQUEST_TIMEOUT_MS = 30_000;

export const httpClient = axios.create({
  baseURL: runtimeConfig.apiBaseUrl,
  timeout: REQUEST_TIMEOUT_MS
});

httpClient.interceptors.request.use((config) => {
  const { token } = getAuthSession();
  config.headers = config.headers ?? {};
  config.headers["Content-Type"] = config.headers["Content-Type"] ?? "application/json";

  if (token) {
    config.headers.Authorization = `Bearer ${token}`;
  }

  return config;
});

function getErrorMessage(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  return "请求失败，请稍后重试";
}

function getPayloadMessage(payload: unknown) {
  if (payload && typeof payload === "object") {
    const data = payload as { error?: unknown; msg?: unknown; message?: unknown; Msg?: unknown };
    const message = data.error ?? data.msg ?? data.message ?? data.Msg;
    if (typeof message === "string" && message) return message;
  }
  return undefined;
}

function normalizeApiResponse<T>(payload: unknown): ApiResponse<T> {
  if (payload && typeof payload === "object" && !Array.isArray(payload) && "Code" in payload) {
    const legacyPayload = payload as {
      Code: number;
      Msg?: string;
      Data?: T;
      data?: T;
    };

    return {
      code: legacyPayload.Code,
      msg: legacyPayload.Msg ?? "",
      data: legacyPayload.Data ?? (legacyPayload.data as T)
    };
  }

  // AIDashboard backend returns raw payloads without an envelope — treat as success.
  if (
    payload &&
    typeof payload === "object" &&
    !Array.isArray(payload) &&
    typeof (payload as { code?: unknown }).code === "number" &&
    (payload as { code?: unknown }).code !== 0
  ) {
    const envelopePayload = payload as { code: number; msg?: string; data?: T };
    return {
      code: envelopePayload.code,
      msg: envelopePayload.msg ?? "",
      data: envelopePayload.data as T
    };
  }

  return { code: 0, msg: "", data: payload as T };
}

function handleHttpError(
  error: AxiosError,
  skipErrorHandler?: boolean,
  silentErrorCodes?: readonly string[]
) {
  const status = error.response?.status;
  const payload = error.response?.data;
  const message = getPayloadMessage(payload) ?? getErrorMessage(error);
  const payloadCode =
    payload && typeof payload === "object" && typeof (payload as { code?: unknown }).code === "string"
      ? (payload as { code: string }).code
      : undefined;
  const suppressErrorFeedback =
    Boolean(skipErrorHandler) || Boolean(payloadCode && silentErrorCodes?.includes(payloadCode));

  if (status === 401) {
    clearAuthSession();
    if (!suppressErrorFeedback) {
      feedback.message()?.warning("登录已失效，请重新登录");
    }
    if (window.location.pathname !== "/login") {
      const next = `${window.location.pathname}${window.location.search}`;
      window.location.assign(`/login?next=${encodeURIComponent(next)}`);
    }
    throw new HttpError("登录已失效", { status, payload });
  }

  if (status === 403) {
    const forbiddenMessage = getPayloadMessage(payload) ?? "暂无访问权限";
    if (!suppressErrorFeedback) {
      feedback.message()?.error(forbiddenMessage);
    }
    throw new HttpError(forbiddenMessage, { status, payload });
  }

  if (!suppressErrorFeedback) {
    if (status && status >= 500) {
      feedback.message()?.error("服务异常，请稍后重试");
    } else if (error.code === "ECONNABORTED") {
      feedback.message()?.error("请求超时，请稍后重试");
    } else {
      feedback.message()?.error(message);
    }
  }

  throw new HttpError(message, { status, payload });
}

export async function request<T>(
  config: AxiosRequestConfig & RequestOptions
): Promise<ApiResponse<T>> {
  const { skipErrorHandler, silentErrorCodes, ...axiosConfig } = config;

  try {
    const response = await httpClient.request<unknown>(axiosConfig);
    const payload = normalizeApiResponse<T>(response.data);

    if (payload.code !== 0) {
      if (!skipErrorHandler) {
        feedback.message()?.error(payload.msg || "业务处理失败");
      }
      throw new HttpError(payload.msg || "业务处理失败", { code: payload.code, payload });
    }

    return payload;
  } catch (error) {
    if (axios.isAxiosError(error)) {
      handleHttpError(error, skipErrorHandler, silentErrorCodes);
    }
    throw error;
  }
}

export const api = {
  get: <T>(url: string, params?: unknown, config?: AxiosRequestConfig & RequestOptions) =>
    request<T>({ ...config, url, method: "GET", params }),
  post: <T>(url: string, data?: unknown, config?: AxiosRequestConfig & RequestOptions) =>
    request<T>({ ...config, url, method: "POST", data }),
  put: <T>(url: string, data?: unknown, config?: AxiosRequestConfig & RequestOptions) =>
    request<T>({ ...config, url, method: "PUT", data }),
  delete: <T>(url: string, data?: unknown, config?: AxiosRequestConfig & RequestOptions) =>
    request<T>({ ...config, url, method: "DELETE", data }),
  patch: <T>(url: string, data?: unknown, config?: AxiosRequestConfig & RequestOptions) =>
    request<T>({ ...config, url, method: "PATCH", data })
};
