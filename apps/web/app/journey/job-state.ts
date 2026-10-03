export const jobStatuses = [
  "queued",
  "planning",
  "drafting",
  "validating",
  "committing",
  "completed",
  "failed",
  "cancelled",
] as const;

export type JobStatus = (typeof jobStatuses)[number];

export type JourneyViewStatus =
  | "ready"
  | "queued"
  | "generating"
  | "saving"
  | "failed"
  | "stopped";

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
  interaction: "act" | "branch" | "continue";
  userAction: string;
  proposal: {
    narrative: string;
    choices: string[];
  };
  achievements?: AchievementAward[];
}

export interface TurnJobSnapshot {
  id: string;
  simulationId: string;
  status: JobStatus;
  errorCode?: string;
  committedTurn?: CommittedTurn;
}

export interface ActiveJobReference {
  simulationId: string;
  jobId: string;
  action?: string;
}

export const activeJobStorageKey = "novelstudio.journey.active-job.v1";

const statusSet = new Set<string>(jobStatuses);

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

export function isJobStatus(value: unknown): value is JobStatus {
  return typeof value === "string" && statusSet.has(value);
}

export function toJourneyViewStatus(status: JobStatus): JourneyViewStatus {
  switch (status) {
    case "queued":
      return "queued";
    case "planning":
    case "drafting":
      return "generating";
    case "validating":
    case "committing":
      return "saving";
    case "failed":
      return "failed";
    case "cancelled":
      return "stopped";
    case "completed":
      return "ready";
  }
}

export function parseTurnJobSnapshot(value: unknown): TurnJobSnapshot | null {
  if (!isRecord(value) || typeof value.id !== "string" || typeof value.simulationId !== "string" || !isJobStatus(value.status)) {
    return null;
  }

  const snapshot: TurnJobSnapshot = {
    id: value.id,
    simulationId: value.simulationId,
    status: value.status,
  };
  if (typeof value.errorCode === "string") snapshot.errorCode = value.errorCode;
  if (isRecord(value.committedTurn)) {
    const turn = value.committedTurn;
    const proposal = turn.proposal;
    if (
      typeof turn.id === "string" &&
      typeof turn.simulationId === "string" &&
      typeof turn.seq === "number" &&
      typeof turn.userAction === "string" &&
      isRecord(proposal) &&
      typeof proposal.narrative === "string" &&
      Array.isArray(proposal.choices) &&
      proposal.choices.every((choice) => typeof choice === "string")
    ) {
      snapshot.committedTurn = {
        id: turn.id,
        simulationId: turn.simulationId,
        seq: turn.seq,
        sceneNo: typeof turn.sceneNo === "number" && turn.sceneNo >= 1 ? turn.sceneNo : 1,
        sceneTitle: typeof turn.sceneTitle === "string" ? turn.sceneTitle : "",
        sceneClosure: typeof turn.sceneClosure === "string" ? turn.sceneClosure : "",
        interaction: turn.interaction === "branch" || turn.interaction === "continue" ? turn.interaction : "act",
        userAction: turn.userAction,
        proposal: { narrative: proposal.narrative, choices: proposal.choices },
      };
      if (Array.isArray(turn.achievements)) {
        const awards: AchievementAward[] = [];
        for (const raw of turn.achievements) {
          if (!isRecord(raw) || typeof raw.title !== "string") continue;
          awards.push({
            scope: raw.scope === "world" || raw.scope === "emergent" || raw.scope === "global" ? raw.scope : "emergent",
            ...(typeof raw.worldId === "string" && raw.worldId ? { worldId: raw.worldId } : {}),
            code: typeof raw.code === "string" ? raw.code : "",
            title: raw.title,
            description: typeof raw.description === "string" ? raw.description : "",
            reward: typeof raw.reward === "number" ? raw.reward : 0,
          });
        }
        if (awards.length > 0) snapshot.committedTurn.achievements = awards;
      }
    }
  }
  return snapshot;
}

export function saveActiveJob(storage: Storage, reference: ActiveJobReference): void {
  storage.setItem(activeJobStorageKey, JSON.stringify(reference));
}

export function restoreActiveJob(storage: Storage, simulationId: string): ActiveJobReference | null {
  const raw = storage.getItem(activeJobStorageKey);
  if (!raw) return null;
  try {
    const value: unknown = JSON.parse(raw);
    if (!isRecord(value) || value.simulationId !== simulationId || typeof value.jobId !== "string" || value.jobId.length === 0) {
      return null;
    }
    return { simulationId, jobId: value.jobId, ...(typeof value.action === "string" && value.action.trim() ? { action: value.action } : {}) };
  } catch {
    return null;
  }
}

export function clearActiveJob(storage: Storage): void {
  storage.removeItem(activeJobStorageKey);
}
