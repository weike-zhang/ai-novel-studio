export interface ApiErrorBody {
  code?: unknown;
  message?: unknown;
  request_id?: unknown;
  [key: string]: unknown;
}

export interface ApiErrorOptions {
  status: number;
  code: string;
  message: string;
  requestId?: string;
  body?: unknown;
  cause?: unknown;
}

/** One error shape for HTTP, malformed-response, and network failures. */
export class ApiError extends Error {
  readonly status: number;
  readonly code: string;
  readonly requestId?: string;
  readonly body?: unknown;

  constructor(options: ApiErrorOptions) {
    super(options.message, { cause: options.cause });
    this.name = "ApiError";
    this.status = options.status;
    this.code = options.code;
    this.requestId = options.requestId;
    this.body = options.body;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export async function toApiError(response: Response): Promise<ApiError> {
  const contentType = response.headers.get("content-type") ?? "";
  let body: unknown;

  try {
    body = contentType.includes("json")
      ? await response.json()
      : (await response.text()) || undefined;
  } catch {
    body = undefined;
  }

  const errorBody: ApiErrorBody = isRecord(body) ? body : {};
  const code =
    typeof errorBody.code === "string" && errorBody.code.length > 0
      ? errorBody.code
      : `http_${response.status}`;
  const message =
    typeof errorBody.message === "string" && errorBody.message.length > 0
      ? errorBody.message
      : response.statusText || "API request failed";
  const requestId =
    typeof errorBody.request_id === "string"
      ? errorBody.request_id
      : response.headers.get("x-request-id") ?? undefined;

  return new ApiError({
    status: response.status,
    code,
    message,
    requestId,
    body,
  });
}
