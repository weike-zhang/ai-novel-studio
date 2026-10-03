import { ApiClient } from "@ai-novel-studio/api-client";

// All player-facing requests authenticate through the HttpOnly session cookie
// set by the Go API. There is no client-side token or dev-only identity header.
export function createApiClient(): ApiClient {
  return new ApiClient();
}
