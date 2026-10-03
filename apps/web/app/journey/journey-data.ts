import type { SimulationSnapshot } from "@ai-novel-studio/api-client";
import type { CommittedTurn } from "./job-state";
import { splitStoryParagraphs } from "./journey-text";

export type JourneyScene = {
  no: number;
  title: string;
  closure: string;
  turns: CommittedTurn[];
};

export type JourneyData = {
  simulationId: string;
  worldId: string;
  worldTitle: string;
  turnLabel: string;
  chapterTitle: string;
  savedAt: string;
  glimmer: number;
  scene: string[];
  quote: string;
  branches: string[];
  scenes: JourneyScene[];
  state: {
    location: string;
    time: string;
    relationships: { name: string; detail: string; tone: string }[];
    variables: { label: string; value: string }[];
    echoes: string[];
  };
};

function record(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Record<string, unknown> : {};
}

function text(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() ? value.trim() : undefined;
}

function textList(value: unknown): string[] {
  return Array.isArray(value) ? value.map(text).filter((item): item is string => Boolean(item)) : [];
}

function displayValue(value: unknown): string | undefined {
  if (typeof value === "string") return text(value);
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  return undefined;
}

function snapshotRelationships(setting: Record<string, unknown>, snapshot: SimulationSnapshot): JourneyData["state"]["relationships"] {
  const persisted = Object.values(record(snapshot.characters));
  if (persisted.length > 0) {
    return persisted.map((value) => {
      const character = record(value);
      const current = record(character.current);
      const detail = Object.values(current).map(displayValue).find(Boolean) ?? "关系仍在发展";
      return { name: text(character.name) ?? "未命名角色", detail, tone: "neutral" };
    });
  }
  const authored = Array.isArray(setting.characters) ? setting.characters : [];
  return authored.map(record).filter((character) => character.public !== false).map((character) => ({
    name: text(character.name) ?? "未命名角色",
    detail: text(character.relation) ?? text(character.role) ?? "尚未相遇",
    tone: "neutral",
  }));
}

function snapshotVariables(snapshot: SimulationSnapshot): JourneyData["state"]["variables"] {
  const state = record(snapshot.state);
  const variables = Object.entries(state).flatMap(([label, value]) => {
    if (label === "location" || label === "time") return [];
    const displayed = displayValue(value);
    return displayed ? [{ label, value: displayed }] : [];
  });
  const threads = Object.values(record(snapshot.threads)).map(record).filter((thread) => text(thread.status) !== "resolved");
  return [...variables, ...threads.map((thread) => ({ label: "进行中线索", value: text(thread.title) ?? text(thread.key) ?? "未命名线索" }))];
}

function sceneTitle(scene: JourneyScene): string {
  return scene.title.trim() || "旅程开场";
}

/** Groups committed turns into narrative scenes keyed by scene number. Scene 1
 * is the world opening; a later scene's title and closure live on its first
 * turn. Turns are ordered ascending by sequence before grouping. */
export function buildScenes(turns: CommittedTurn[]): JourneyScene[] {
  const ordered = [...turns].sort((a, b) => a.seq - b.seq);
  const scenes: JourneyScene[] = [];
  let current: JourneyScene | null = null;
  for (const turn of ordered) {
    if (!current || turn.sceneNo !== current.no) {
      current = { no: turn.sceneNo, title: turn.sceneTitle ?? "", closure: turn.sceneClosure ?? "", turns: [] };
      scenes.push(current);
    }
    current.turns.push(turn);
  }
  return scenes;
}

export function hydrateJourneyFromSnapshot(base: JourneyData, snapshot: SimulationSnapshot): JourneyData {
  const frozen = record(snapshot.worldSnapshot);
  const world = record(frozen.world);
  const setting = record(frozen.setting);
  const opening = record(setting.opening);
  const background = record(setting.worldBackground);
  const state = record(snapshot.state);
  const openingText = text(opening.text) ?? text(world.summary);
  const scene = openingText ? splitStoryParagraphs(openingText) : base.scene;
  const branches = textList(opening.actions);
  const scenes = buildScenes([...snapshot.recentTurns].sort((a, b) => a.seq - b.seq));
  const currentScene = scenes.at(-1);
  const lastSceneNo = Math.max(1, snapshot.lastSceneNo ?? 1);
  const chapterTitle = currentScene ? sceneTitle(currentScene) : "旅程开场";

  return {
    ...base,
    worldId: text(world.id) ?? base.worldId,
    worldTitle: text(world.title) ?? base.worldTitle,
    turnLabel: `第${lastSceneNo}幕`,
    chapterTitle,
    scene,
    quote: snapshot.lastTurnSeq === 0 ? "" : base.quote,
    branches: branches.length ? branches : base.branches,
    scenes,
    state: {
      ...base.state,
      location: text(state.location) ?? text(background.eraPlace) ?? "",
      time: text(state.time) ?? "",
      relationships: snapshotRelationships(setting, snapshot),
      variables: snapshotVariables(snapshot),
      echoes: snapshot.summary.trim() ? [snapshot.summary.trim()] : [],
    },
  };
}
