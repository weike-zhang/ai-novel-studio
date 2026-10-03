import type { AuthoringWorld, CommittedTurn } from "./client.js";
import type { SseEvent } from "./sse.js";

export type AIJobStatus =
  | "queued"
  | "planning"
  | "drafting"
  | "validating"
  | "committing"
  | "completed"
  | "failed"
  | "cancelled";

export type ActiveAIJobStatus = Exclude<AIJobStatus, "completed" | "failed" | "cancelled">;

export interface GenerateWorldDraftInput {
  topic: string;
  type: string;
  contentRating: "全年龄" | "13+" | "16+" | "18+";
  boundaries: string;
}

export interface CreateWorldDraftJobOptions {
  idempotencyKey: string;
  signal?: AbortSignal;
}

interface AIJobBase {
  id: string;
  kind: "turn" | "world_draft";
  status: AIJobStatus;
  attemptNo: number;
  canCancel: boolean;
  canRetry: boolean;
  queuePosition?: number;
  errorCode?: string;
  createdAt: string;
  updatedAt: string;
}

export type GeneratedPrivateWorld = Omit<
  AuthoringWorld,
  | "visibility"
  | "status"
  | "draftSource"
  | "humanConfirmedVersion"
  | "humanConfirmedFields"
  | "humanConfirmedAt"
  | "publishedAt"
  | "unpublishedAt"
> & {
  visibility: "private";
  status: "draft";
  draftSource: "ai";
  humanConfirmedVersion?: never;
  humanConfirmedFields?: never;
  humanConfirmedAt?: never;
  publishedAt?: never;
  unpublishedAt?: never;
};

interface WorldDraftJobBase extends AIJobBase {
  kind: "world_draft";
}

export interface ActiveWorldDraftJob extends WorldDraftJobBase {
  status: ActiveAIJobStatus;
  result?: never;
}

export interface CompletedWorldDraftJob extends WorldDraftJobBase {
  status: "completed";
  canCancel: false;
  canRetry: false;
  result: { world: GeneratedPrivateWorld };
}

export interface FailedWorldDraftJob extends WorldDraftJobBase {
  status: "failed";
  canCancel: false;
  result?: never;
  errorCode: string;
}

export interface CancelledWorldDraftJob extends WorldDraftJobBase {
  status: "cancelled";
  canCancel: false;
  result?: never;
}

export type WorldDraftJob =
  | ActiveWorldDraftJob
  | CompletedWorldDraftJob
  | FailedWorldDraftJob
  | CancelledWorldDraftJob;

interface TurnJobBase extends AIJobBase {
  kind: "turn";
  simulationId: string;
}

export interface ActiveTurnJob extends TurnJobBase {
  status: ActiveAIJobStatus;
  committedTurn?: never;
}

export interface CompletedTurnJob extends TurnJobBase {
  status: "completed";
  canCancel: false;
  canRetry: false;
  committedTurn: CommittedTurn;
}

export interface FailedTurnJob extends TurnJobBase {
  status: "failed";
  canCancel: false;
  committedTurn?: never;
  errorCode: string;
}

export interface CancelledTurnJob extends TurnJobBase {
  status: "cancelled";
  canCancel: false;
  committedTurn?: never;
}

export type TurnJob = ActiveTurnJob | CompletedTurnJob | FailedTurnJob | CancelledTurnJob;
export type AIJob = TurnJob | WorldDraftJob;

export interface JobStatusEventData {
  jobId: string;
  kind: AIJob["kind"];
  status: AIJobStatus;
  attemptNo: number;
  queuePosition?: number;
  canCancel: boolean;
  errorCode?: string;
}

export type AIJobEvent =
  | (SseEvent<JobStatusEventData> & { event: "job.status" })
  | (SseEvent<{ jobId: string; worldId: string }> & { event: "world_draft.completed" })
  | (SseEvent<CommittedTurn> & { event: "turn.committed" })
  | (SseEvent<{ code: string }> & { event: "error" });
