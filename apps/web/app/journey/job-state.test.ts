import { describe, expect, it } from "vitest";
import {
  activeJobStorageKey,
  clearActiveJob,
  jobStatuses,
  parseTurnJobSnapshot,
  restoreActiveJob,
  saveActiveJob,
  toJourneyViewStatus,
} from "./job-state";

function memoryStorage(): Storage {
  const values = new Map<string, string>();
  return {
    get length() { return values.size; },
    clear: () => values.clear(),
    getItem: (key) => values.get(key) ?? null,
    key: (index) => [...values.keys()][index] ?? null,
    removeItem: (key) => { values.delete(key); },
    setItem: (key, value) => { values.set(key, value); },
  };
}

describe("journey job status mapping", () => {
  it("maps every backend state to an existing journey presentation state", () => {
    expect(jobStatuses.map(toJourneyViewStatus)).toEqual([
      "queued", "generating", "generating", "saving", "saving", "ready", "failed", "stopped",
    ]);
  });

  it("only accepts committed narrative after a completed response shape is valid", () => {
    const parsed = parseTurnJobSnapshot({
      id: "job-1",
      simulationId: "simulation-1",
      status: "completed",
      committedTurn: {
        id: "turn-1",
        simulationId: "simulation-1",
        seq: 13,
        sceneNo: 2,
        sceneTitle: "雨巷",
        sceneClosure: "你离开旧港，一夜之后抵达落雨的窄巷。",
        userAction: "推开门",
        proposal: { narrative: "门外的雾散开了。", choices: ["继续向前"] },
      },
    });
    expect(parsed?.committedTurn?.proposal.narrative).toBe("门外的雾散开了。");
    expect(parsed?.committedTurn).toMatchObject({ sceneNo: 2, sceneTitle: "雨巷", sceneClosure: "你离开旧港，一夜之后抵达落雨的窄巷。" });
    expect(parseTurnJobSnapshot({ id: "job-1", simulationId: "simulation-1", status: "mystery" })).toBeNull();
  });

  it("tolerates a committed turn without scene metadata", () => {
    const parsed = parseTurnJobSnapshot({
      id: "job-2",
      simulationId: "simulation-1",
      status: "completed",
      committedTurn: {
        id: "turn-1",
        simulationId: "simulation-1",
        seq: 13,
        userAction: "推开门",
        proposal: { narrative: "门外的雾散开了。", choices: ["继续向前"] },
      },
    });
    expect(parsed?.committedTurn).toMatchObject({ sceneNo: 1, sceneTitle: "", sceneClosure: "" });
  });
});

describe("active job recovery", () => {
  it("stores only the simulation and job identifiers", () => {
    const storage = memoryStorage();
    saveActiveJob(storage, { simulationId: "simulation-1", jobId: "job-1" });
    expect(storage.getItem(activeJobStorageKey)).toBe('{"simulationId":"simulation-1","jobId":"job-1"}');
    expect(restoreActiveJob(storage, "simulation-1")).toEqual({ simulationId: "simulation-1", jobId: "job-1" });
    clearActiveJob(storage);
    expect(storage.length).toBe(0);
  });

  it("persists the action so a failed job can be recovered after reload", () => {
    const storage = memoryStorage();
    saveActiveJob(storage, { simulationId: "simulation-1", jobId: "job-1", action: "追踪脉冲" });
    expect(restoreActiveJob(storage, "simulation-1")).toEqual({ simulationId: "simulation-1", jobId: "job-1", action: "追踪脉冲" });
  });

  it("ignores another journey and malformed storage", () => {
    const storage = memoryStorage();
    saveActiveJob(storage, { simulationId: "simulation-1", jobId: "job-1" });
    expect(restoreActiveJob(storage, "simulation-2")).toBeNull();
    storage.setItem(activeJobStorageKey, "not-json");
    expect(restoreActiveJob(storage, "simulation-1")).toBeNull();
  });
});
