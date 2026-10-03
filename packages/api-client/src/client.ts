import { ApiError, toApiError } from "./error.js";
import type {
  AIJob,
  AIJobEvent,
  CreateWorldDraftJobOptions,
  GenerateWorldDraftInput,
  TurnJob,
  WorldDraftJob,
} from "./jobs.js";
import { createSseParser } from "./sse.js";

export interface ApiContract {
  login: LoginResult;
  logout: void;
  logoutAll: void;
  changePassword: void;
  rotateRecoveryCode: RotateRecoveryCodeResult;
  sendEmailVerificationCode: EmailVerificationCodeResponse;
  register: RegistrationResult;
  getMe: Me;
  checkIn: CheckInResult;
  getBenefits: Benefits;
  getCheckins: CheckInList;
  listSessions: SessionList;
  revokeSession: void;
  listMySimulations: SimulationList;
  getMyAchievements: AchievementList;
  listMyWorlds: MyWorldList;
  listWorlds: WorldPage;
  getWorld: World;
  createSimulation: Simulation;
  getSimulation: SimulationSnapshot;
  createWorld: AuthoringWorld;
  updateWorld: AuthoringWorld;
  confirmAIWorldFields: AuthoringWorld;
  publishWorld: AuthoringWorld;
  unpublishWorld: AuthoringWorld;
  generateCover: CoverResult;
  uploadCover: CoverResult;
  regenerateWorldCover: { queued: boolean };
  regenerateAllCovers: { queued: number };
  setWorldFeatured: { featured: boolean; rank: number };
  removeWorldFeatured: { featured: boolean; rank: number };
  reportWorld: WorldReport;
  generateWorldDraft: WorldDraftJob;
  aiAssist: AssistResult;
  createTurnJob: TurnJob;
  getJob: AIJob;
  cancelJob: AIJob;
  retryJob: AIJob;
  getIsAdmin: AdminStatus;
  suspendWorld: AuthoringWorld;
  restoreWorld: AuthoringWorld;
}

export interface LoginResult {
  userId: string;
  accountName: string;
}

export interface LoginInput {
  accountName: string;
  password: string;
}

export interface RotateRecoveryCodeResult {
  recoveryCode: string;
}

export interface ChangePasswordInput {
  currentPassword: string;
  replacementPassword: string;
}

export interface RotateRecoveryCodeInput {
  currentPassword: string;
}

export interface Me {
  userId: string;
  accountName: string;
  nickname: string;
  email: string;
  emailVerified: boolean;
  adultConfirmed: boolean;
  termsVersion: string;
  status: string;
  availableBalance: number;
  createdAt: string;
  recoveryCodeSet: boolean;
}

export interface CheckInResult {
  streakDays: number;
  rewardCredits: number;
  bonusCredits: number;
  alreadyToday: boolean;
}

export interface Benefits {
  credits: number;
  coverAllowance: number;
}

export interface CheckInRecord {
  date: string;
  streakDays: number;
  rewardCredits: number;
  bonusCredits: number;
}

export interface CheckInList {
  checkins: CheckInRecord[];
}

export interface SessionSummary {
  id: string;
  familyId: string;
  createdAt: string;
  lastUsedAt: string;
  current: boolean;
}

export interface SessionList {
  sessions: SessionSummary[];
}

export interface RevokeSessionInput {
  sessionId: string;
}

export interface SimulationSummary {
  id: string;
  worldId: string;
  worldTitle: string;
  summary: string;
  status: string;
  lastTurnSeq: number;
  lastSceneNo: number;
  updatedAt: string;
}

export interface SimulationList {
  simulations: SimulationSummary[];
}

export interface Achievement {
  scope: "global" | "world" | "emergent";
  worldId?: string;
  worldTitle?: string;
  code: string;
  title: string;
  description: string;
  reward: number;
  createdAt: string;
}

export interface AchievementList {
  achievements: Achievement[];
}

export interface MyWorldSummary {
  id: string;
  title: string;
  summary: string;
  visibility: "private" | "public";
  status: "draft" | "pending_review" | "published" | "suspended" | "archived";
  version: number;
  simulationCount: number;
  updatedAt: string;
}

export interface MyWorldList {
  worlds: MyWorldSummary[];
}

export interface EmailVerificationCodeResponse {
  nonce: string;
  cooldownSeconds: number;
  /** Present only in development when the server echoes the synthetic code. */
  verificationCode?: string;
}

export interface SendEmailVerificationCodeInput {
  email: string;
  turnstileToken: string;
}

export interface RegistrationResult {
  userId: string;
  accountName: string;
  recoveryCode: string;
  grantedLight: number;
}

export interface RegisterInput {
  accountName: string;
  password: string;
  email: string;
  code: string;
  nonce: string;
  turnstileToken: string;
  adultConfirmed: boolean;
  termsVersion: string;
}

export interface WorldCard {
  id: string;
  ownerId: string;
  authorNickname: string;
  title: string;
  summary: string;
  hook?: string;
  promise?: string;
  playTime?: string;
  format?: string;
  featuredRank?: number;
  cover?: string;
  type?: string;
  tags: string[];
  simulationCount: number;
  version: number;
  updatedAt: string;
}

export interface World extends WorldCard {
  visibility: "private" | "public";
  status: "draft" | "pending_review" | "published" | "suspended" | "archived";
  setting: Record<string, unknown>;
}

export interface WorldPage {
  worlds: WorldCard[];
  nextCursor?: string;
}

export interface Simulation {
  id: string;
  userId: string;
  worldId: string;
  worldVersion: number;
  worldSnapshot: Record<string, unknown>;
  status: string;
  version: number;
  createdAt: string;
}

export interface AchievementAward {
  scope: "global" | "world" | "emergent";
  worldId?: string;
  code: string;
  title: string;
  description: string;
  reward: number;
}

export interface CommittedTurn {
  id: string;
  simulationId: string;
  seq: number;
  sceneNo: number;
  sceneTitle: string;
  sceneClosure: string;
  interaction: TurnInteraction;
  userAction: string;
  proposal: {
    narrative: string;
    choices: string[];
    [key: string]: unknown;
  };
  status: "committed";
  achievements?: AchievementAward[];
  createdAt: string;
}

export interface SimulationSnapshot {
  id: string;
  worldId: string;
  worldSnapshot: Record<string, unknown>;
  state: Record<string, unknown>;
  summary: string;
  version: number;
  lastTurnSeq: number;
  lastSceneNo: number;
  characters: Record<string, unknown>;
  threads: Record<string, unknown>;
  recentTurns: CommittedTurn[];
}

export type DraftSource = "manual" | "ai";

export interface AuthoringWorld {
  id: string;
  ownerId: string;
  title: string;
  summary: string;
  setting: Record<string, unknown>;
  visibility: "private" | "public";
  status: "draft" | "pending_review" | "published";
  version: number;
  draftSource: DraftSource;
  humanConfirmedVersion?: number;
  humanConfirmedFields?: string[];
  humanConfirmedAt?: string;
  publishedAt?: string;
  unpublishedAt?: string;
  createdAt: string;
  updatedAt: string;
}

export interface CreateWorldDraftInput { title: string; summary: string; setting: Record<string, unknown>; source: DraftSource }
export interface UpdateWorldDraftInput { expectedVersion: number; title: string; summary: string; setting: Record<string, unknown> }
export interface ConfirmAIWorldFieldsInput { expectedVersion: number; fields: string[] }
export interface ChangeWorldPublicationInput { expectedVersion: number }
export interface CoverResult {
  cover: string;
}
export interface AdminStatus {
  isAdmin: boolean;
}
export type AssistAction = "inspire" | "polish";
export interface AssistInput {
  action: AssistAction;
  field: string;
  current?: string;
  contextHint?: string;
}
export interface AssistResult {
  text: string;
}
export type WorldReportReason = "违法违规" | "色情低俗" | "暴力仇恨" | "侵权冒用" | "其他";
export interface CreateWorldReportInput { reason: WorldReportReason; note?: string }
export interface WorldReport {
  id: string;
  worldId: string;
  reporterId: string;
  reason: WorldReportReason;
  note?: string;
  status: "pending" | "reviewing" | "resolved" | "dismissed";
  createdAt: string;
  updatedAt: string;
}

export type WorldSort = "newest" | "popular" | "recommended";

export interface ListWorldsQuery {
  keyword?: string;
  type?: string;
  /** Tags are serialized as the comma-separated value required by OpenAPI. */
  tags?: readonly string[] | string;
  sort?: WorldSort;
  cursor?: string;
  limit?: number;
}

export interface CreateSimulationInput {
  worldId: string;
}

export type TurnInteraction = "act" | "branch" | "continue";

export interface CreateTurnJobInput {
  action: string;
  interaction?: TurnInteraction;
}

export interface CreateTurnJobOptions {
  idempotencyKey: string;
  signal?: AbortSignal;
}

export interface RequestOptions {
  signal?: AbortSignal;
}

export interface ApiClientOptions {
  /** Defaults to the OpenAPI server prefix. May be relative or absolute. */
  baseUrl?: string;
  /** Primarily for tests or server-side adapters. No credential/token option is exposed. */
  fetch?: typeof globalThis.fetch;
}

function trimTrailingSlashes(value: string): string {
  return value.replace(/\/+$/, "");
}

function resourcePath(value: string): string {
  return encodeURIComponent(value);
}

function worldQueryString(query: ListWorldsQuery): string {
  const parameters = new URLSearchParams();
  if (query.keyword !== undefined) parameters.set("keyword", query.keyword);
  if (query.type !== undefined) parameters.set("type", query.type);
  if (query.tags !== undefined) {
    parameters.set("tags", typeof query.tags === "string" ? query.tags : query.tags.join(","));
  }
  if (query.sort !== undefined) parameters.set("sort", query.sort);
  if (query.cursor !== undefined) parameters.set("cursor", query.cursor);
  if (query.limit !== undefined) parameters.set("limit", String(query.limit));
  const encoded = parameters.toString();
  return encoded === "" ? "" : `?${encoded}`;
}

function validateIdempotencyKey(value: string): void {
  if (value.length < 16 || value.length > 128) {
    throw new TypeError("Idempotency-Key must contain 16 to 128 characters");
  }
}

export function createIdempotencyKey(): string {
  const crypto = globalThis.crypto;
  if (typeof crypto?.randomUUID === "function") {
    return crypto.randomUUID();
  }
  const bytes = new Uint8Array(16);
  if (typeof crypto?.getRandomValues === "function") {
    crypto.getRandomValues(bytes);
  } else {
    for (let index = 0; index < bytes.length; index += 1) {
      bytes[index] = Math.floor(Math.random() * 256);
    }
  }
  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, (byte) => byte.toString(16).padStart(2, "0")).join("");
  return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}

export class ApiClient<Responses extends ApiContract = ApiContract> {
  readonly baseUrl: string;
  private readonly fetcher: typeof globalThis.fetch;

  constructor(options: ApiClientOptions = {}) {
    this.baseUrl = trimTrailingSlashes(options.baseUrl ?? "/v1");
    this.fetcher = options.fetch ?? globalThis.fetch?.bind(globalThis);
    if (!this.fetcher) throw new Error("A Fetch API implementation is required");
  }

  listWorlds(
    query: ListWorldsQuery = {},
    options: RequestOptions = {},
  ): Promise<Responses["listWorlds"]> {
    return this.request<Responses["listWorlds"]>(`/worlds${worldQueryString(query)}`, options);
  }

  login(input: LoginInput, options: RequestOptions = {}): Promise<Responses["login"]> {
    return this.request<Responses["login"]>("/auth/login", { method: "POST", body: JSON.stringify(input), signal: options.signal });
  }

  logout(options: RequestOptions = {}): Promise<Responses["logout"]> {
    return this.request<Responses["logout"]>("/auth/logout", { method: "POST", signal: options.signal });
  }

  logoutAll(options: RequestOptions = {}): Promise<Responses["logoutAll"]> {
    return this.request<Responses["logoutAll"]>("/auth/logout-all", { method: "POST", signal: options.signal });
  }

  changePassword(input: ChangePasswordInput, options: RequestOptions = {}): Promise<Responses["changePassword"]> {
    return this.request<Responses["changePassword"]>("/auth/password/change", { method: "POST", body: JSON.stringify(input), signal: options.signal });
  }

  rotateRecoveryCode(input: RotateRecoveryCodeInput, options: RequestOptions = {}): Promise<Responses["rotateRecoveryCode"]> {
    return this.request<Responses["rotateRecoveryCode"]>("/auth/recovery-code/rotate", { method: "POST", body: JSON.stringify(input), signal: options.signal });
  }

  getMe(options: RequestOptions = {}): Promise<Responses["getMe"]> {
    return this.request<Responses["getMe"]>("/me", { signal: options.signal });
  }

  checkIn(options: RequestOptions = {}): Promise<Responses["checkIn"]> {
    return this.request<Responses["checkIn"]>("/me/checkin", { method: "POST", signal: options.signal });
  }

  getBenefits(options: RequestOptions = {}): Promise<Responses["getBenefits"]> {
    return this.request<Responses["getBenefits"]>("/me/benefits", { signal: options.signal });
  }

  getCheckins(options: RequestOptions = {}): Promise<Responses["getCheckins"]> {
    return this.request<Responses["getCheckins"]>("/me/checkins", { signal: options.signal });
  }

  listSessions(options: RequestOptions = {}): Promise<Responses["listSessions"]> {
    return this.request<Responses["listSessions"]>("/me/sessions", { signal: options.signal });
  }

  revokeSession(input: RevokeSessionInput, options: RequestOptions = {}): Promise<Responses["revokeSession"]> {
    return this.request<Responses["revokeSession"]>("/me/sessions/revoke", { method: "POST", body: JSON.stringify(input), signal: options.signal });
  }

  listMySimulations(options: RequestOptions = {}): Promise<Responses["listMySimulations"]> {
    return this.request<Responses["listMySimulations"]>("/me/simulations", { signal: options.signal });
  }

  getMyAchievements(options: RequestOptions = {}): Promise<Responses["getMyAchievements"]> {
    return this.request<Responses["getMyAchievements"]>("/me/achievements", { signal: options.signal });
  }

  listMyWorlds(options: RequestOptions = {}): Promise<Responses["listMyWorlds"]> {
    return this.request<Responses["listMyWorlds"]>("/me/worlds", { signal: options.signal });
  }

  sendEmailVerificationCode(
    input: SendEmailVerificationCodeInput,
    options: RequestOptions = {},
  ): Promise<Responses["sendEmailVerificationCode"]> {
    return this.request<Responses["sendEmailVerificationCode"]>("/auth/email-verification-codes", {
      method: "POST",
      body: JSON.stringify(input),
      signal: options.signal,
    });
  }

  register(
    input: RegisterInput,
    options: RequestOptions = {},
  ): Promise<Responses["register"]> {
    return this.request<Responses["register"]>("/auth/register", {
      method: "POST",
      body: JSON.stringify(input),
      signal: options.signal,
    });
  }

  getWorld(worldId: string, options: RequestOptions = {}): Promise<Responses["getWorld"]> {
    return this.request<Responses["getWorld"]>(`/worlds/${resourcePath(worldId)}`, options);
  }

  createSimulation(
    input: CreateSimulationInput,
    options: RequestOptions = {},
  ): Promise<Responses["createSimulation"]> {
    return this.request<Responses["createSimulation"]>("/simulations", {
      method: "POST",
      body: JSON.stringify(input),
      signal: options.signal,
    });
  }

  /** Product-language alias for createSimulation. */
  createJourney(
    input: CreateSimulationInput,
    options: RequestOptions = {},
  ): Promise<Responses["createSimulation"]> {
    return this.createSimulation(input, options);
  }

  getSimulation(simulationId: string, options: RequestOptions = {}): Promise<Responses["getSimulation"]> {
    return this.request<Responses["getSimulation"]>(`/simulations/${resourcePath(simulationId)}`, options);
  }

  createWorld(input: CreateWorldDraftInput, options: RequestOptions = {}): Promise<Responses["createWorld"]> {
    return this.request<Responses["createWorld"]>("/worlds", { method: "POST", body: JSON.stringify(input), signal: options.signal });
  }

  updateWorld(worldId: string, input: UpdateWorldDraftInput, options: RequestOptions = {}): Promise<Responses["updateWorld"]> {
    return this.request<Responses["updateWorld"]>(`/worlds/${resourcePath(worldId)}`, { method: "PATCH", body: JSON.stringify(input), signal: options.signal });
  }

  confirmAIWorldFields(worldId: string, input: ConfirmAIWorldFieldsInput, options: RequestOptions = {}): Promise<Responses["confirmAIWorldFields"]> {
    return this.request<Responses["confirmAIWorldFields"]>(`/worlds/${resourcePath(worldId)}/confirm-ai-fields`, { method: "POST", body: JSON.stringify(input), signal: options.signal });
  }

  publishWorld(worldId: string, input: ChangeWorldPublicationInput, options: RequestOptions = {}): Promise<Responses["publishWorld"]> {
    return this.request<Responses["publishWorld"]>(`/worlds/${resourcePath(worldId)}/publish`, { method: "POST", body: JSON.stringify(input), signal: options.signal });
  }

  unpublishWorld(worldId: string, input: ChangeWorldPublicationInput, options: RequestOptions = {}): Promise<Responses["unpublishWorld"]> {
    return this.request<Responses["unpublishWorld"]>(`/worlds/${resourcePath(worldId)}/unpublish`, { method: "POST", body: JSON.stringify(input), signal: options.signal });
  }

  generateCover(worldId: string, options: RequestOptions = {}): Promise<Responses["generateCover"]> {
    return this.request<Responses["generateCover"]>(`/worlds/${resourcePath(worldId)}/cover`, { method: "POST", signal: options.signal });
  }

  uploadCover(worldId: string, body: Blob, contentType: string, options: RequestOptions = {}): Promise<Responses["uploadCover"]> {
    return this.request<Responses["uploadCover"]>(`/worlds/${resourcePath(worldId)}/cover/upload`, {
      method: "POST",
      headers: { "Content-Type": contentType },
      body,
      signal: options.signal,
    });
  }

  regenerateWorldCover(worldId: string, options: RequestOptions = {}): Promise<Responses["regenerateWorldCover"]> {
    return this.request<Responses["regenerateWorldCover"]>(`/worlds/${resourcePath(worldId)}/cover/generate`, { method: "POST", signal: options.signal });
  }

  regenerateAllCovers(options: RequestOptions = {}): Promise<Responses["regenerateAllCovers"]> {
    return this.request<Responses["regenerateAllCovers"]>("/worlds/covers/regenerate-missing", { method: "POST", signal: options.signal });
  }

  setWorldFeatured(worldId: string, options: RequestOptions = {}): Promise<Responses["setWorldFeatured"]> {
    return this.request<Responses["setWorldFeatured"]>(`/worlds/${resourcePath(worldId)}/featured`, { method: "POST", signal: options.signal });
  }

  removeWorldFeatured(worldId: string, options: RequestOptions = {}): Promise<Responses["removeWorldFeatured"]> {
    return this.request<Responses["removeWorldFeatured"]>(`/worlds/${resourcePath(worldId)}/featured/remove`, { method: "POST", signal: options.signal });
  }

  reportWorld(worldId: string, input: CreateWorldReportInput, options: RequestOptions = {}): Promise<Responses["reportWorld"]> {
    return this.request<Responses["reportWorld"]>(`/worlds/${resourcePath(worldId)}/reports`, { method: "POST", body: JSON.stringify(input), signal: options.signal });
  }

  getIsAdmin(options: RequestOptions = {}): Promise<Responses["getIsAdmin"]> {
    return this.request<Responses["getIsAdmin"]>("/me/is-admin", { signal: options.signal });
  }

  suspendWorld(worldId: string, options: RequestOptions = {}): Promise<Responses["suspendWorld"]> {
    return this.request<Responses["suspendWorld"]>(`/worlds/${resourcePath(worldId)}/suspend`, { method: "POST", signal: options.signal });
  }

  restoreWorld(worldId: string, options: RequestOptions = {}): Promise<Responses["restoreWorld"]> {
    return this.request<Responses["restoreWorld"]>(`/worlds/${resourcePath(worldId)}/restore`, { method: "POST", signal: options.signal });
  }

  aiAssist(input: AssistInput, options: RequestOptions = {}): Promise<Responses["aiAssist"]> {
    return this.request<Responses["aiAssist"]>("/worlds/ai-assist", { method: "POST", body: JSON.stringify(input), signal: options.signal });
  }

  createTurnJob(
    simulationId: string,
    input: CreateTurnJobInput,
    options: CreateTurnJobOptions,
  ): Promise<Responses["createTurnJob"]> {
    validateIdempotencyKey(options.idempotencyKey);
    return this.request<Responses["createTurnJob"]>(`/simulations/${resourcePath(simulationId)}/turn-jobs`, {
      method: "POST",
      headers: { "Idempotency-Key": options.idempotencyKey },
      body: JSON.stringify(input),
      signal: options.signal,
    });
  }

  generateWorldDraft(
    input: GenerateWorldDraftInput,
    options: CreateWorldDraftJobOptions,
  ): Promise<Responses["generateWorldDraft"]> {
    validateIdempotencyKey(options.idempotencyKey);
    return this.request<Responses["generateWorldDraft"]>("/worlds/generate-draft", {
      method: "POST",
      headers: { "Idempotency-Key": options.idempotencyKey },
      body: JSON.stringify(input),
      signal: options.signal,
    });
  }

  getJob(jobId: string, options: RequestOptions = {}): Promise<Responses["getJob"]> {
    return this.request<Responses["getJob"]>(`/jobs/${resourcePath(jobId)}`, options);
  }

  cancelJob(jobId: string, options: RequestOptions = {}): Promise<Responses["cancelJob"]> {
    return this.request<Responses["cancelJob"]>(`/jobs/${resourcePath(jobId)}/cancel`, {
      method: "POST",
      signal: options.signal,
    });
  }

  retryJob(jobId: string, options: RequestOptions = {}): Promise<Responses["retryJob"]> {
    return this.request<Responses["retryJob"]>(`/jobs/${resourcePath(jobId)}/retry`, {
      method: "POST",
      signal: options.signal,
    });
  }

  getJobEventsUrl(jobId: string): string {
    return `${this.baseUrl}/jobs/${resourcePath(jobId)}/events`;
  }

  /**
   * Stream job events over SSE. Resolves when the stream ends, rejects on
   * network or HTTP errors, and calls onEvent for each parsed event.
   */
  async streamJobEvents(
    jobId: string,
    onEvent: (event: AIJobEvent) => void,
    options: RequestOptions = {},
  ): Promise<void> {
    const response = await this.fetcher(this.getJobEventsUrl(jobId), {
      method: "GET",
      credentials: "same-origin",
      signal: options.signal,
      headers: { Accept: "text/event-stream" },
    });
    if (!response.ok) throw await toApiError(response);
    if (!response.body) {
      throw new ApiError({ status: 0, code: "sse_unavailable", message: "This environment cannot stream job events" });
    }
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    const parser = createSseParser<AIJobEvent["data"]>();
    try {
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        for (const parsed of parser.push(decoder.decode(value, { stream: true }))) {
          onEvent(parsed as AIJobEvent);
        }
      }
      for (const parsed of parser.finish()) onEvent(parsed as AIJobEvent);
    } finally {
      reader.releaseLock();
    }
  }

  private async request<T>(path: string, init: RequestInit): Promise<T> {
    let response: Response;
    try {
      response = await this.fetcher(`${this.baseUrl}${path}`, {
        ...init,
        credentials: "same-origin",
        headers: {
          Accept: "application/json",
          ...(init.body === undefined ? {} : { "Content-Type": "application/json" }),
          ...init.headers,
        },
      });
    } catch (cause) {
      throw new ApiError({
        status: 0,
        code: "network_error",
        message: "Unable to reach the API",
        cause,
      });
    }

    if (!response.ok) throw await toApiError(response);
    if (response.status === 204) return undefined as T;

    try {
      const text = await response.text();
      return (text === "" ? undefined : JSON.parse(text)) as T;
    } catch (cause) {
      throw new ApiError({
        status: response.status,
        code: "invalid_response",
        message: "API returned an invalid JSON response",
        requestId: response.headers.get("x-request-id") ?? undefined,
        cause,
      });
    }
  }
}
