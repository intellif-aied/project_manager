export interface ApiResponse<T> {
  code: number;
  msg: string;
  data: T;
}

export interface RequestOptions {
  skipErrorHandler?: boolean;
  silentErrorCodes?: readonly string[];
}

export class HttpError extends Error {
  status?: number;
  code?: number;
  payload?: unknown;

  constructor(message: string, options?: { status?: number; code?: number; payload?: unknown }) {
    super(message);
    this.name = "HttpError";
    this.status = options?.status;
    this.code = options?.code;
    this.payload = options?.payload;
  }
}
