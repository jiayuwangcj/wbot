import type { JsonObject } from "./types";

export interface ApiErrorPayload {
  code?: string;
  message?: string;
  action?: string;
  error?: string;
}

export class ApiError extends Error {
  readonly code: string;
  readonly action: string;
  readonly status: number;

  constructor(message: string, status: number, code = "request_failed", action = "") {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.action = action;
    this.status = status;
  }
}

function isObject(value: unknown): value is JsonObject {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function readErrorPayload(value: unknown): ApiErrorPayload {
  if (!isObject(value)) return {};
  const payload: ApiErrorPayload = {};
  if (typeof value.code === "string") payload.code = value.code;
  if (typeof value.message === "string") payload.message = value.message;
  if (typeof value.action === "string") payload.action = value.action;
  if (typeof value.error === "string") payload.error = value.error;
  return payload;
}

function errorMessage(payload: ApiErrorPayload, status: number): string {
  const message = payload.message || payload.error || `HTTP ${status}`;
  return payload.action ? `${message} · ${payload.action}` : message;
}

export async function fetchJSON<T>(url: string, init?: RequestInit): Promise<T> {
  let response: Response;
  try {
    response = await fetch(url, init);
  } catch {
    throw new ApiError("cannot reach the server", 0, "network_error", "check the server and retry");
  }
  const text = await response.text();
  let body: unknown = undefined;
  if (text !== "") {
    try {
      body = JSON.parse(text) as unknown;
    } catch {
      if (!response.ok) {
        throw new ApiError(`HTTP ${response.status}`, response.status);
      }
      throw new ApiError("unexpected server response", response.status, "invalid_response", "retry the request");
    }
  }
  if (!response.ok) {
    const payload = readErrorPayload(body);
    throw new ApiError(errorMessage(payload, response.status), response.status, payload.code, payload.action);
  }
  return body as T;
}
