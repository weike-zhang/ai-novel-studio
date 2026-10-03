import { describe, expect, it } from "vitest";
import { buildScenes, hydrateJourneyFromSnapshot, type JourneyData } from "./journey-data";

const base: JourneyData = {
  simulationId: "simulation-1", worldId: "world-1", worldTitle: "演示世界", turnLabel: "第十二幕", chapterTitle: "演示章节",
  savedAt: "刚刚", glimmer: 24, scene: ["演示正文"], quote: "演示引语", branches: ["演示行动"], scenes: [],
  state: { location: "演示地点", time: "演示时间", relationships: [], variables: [], echoes: [] },
};

describe("journey snapshot hydration", () => {
  it("uses the frozen authored world for a new private test journey", () => {
    const result = hydrateJourneyFromSnapshot(base, {
      id: "simulation-1", worldId: "world-1", version: 1, lastTurnSeq: 0, lastSceneNo: 0, summary: "", state: {}, characters: {}, threads: {}, recentTurns: [],
      worldSnapshot: {
        world: { title: "联调世界", summary: "冻结简介" },
        setting: {
          worldBackground: { eraPlace: "雾港" },
          opening: { text: "第一段。\n\n第二段。", actions: ["前往灯塔", "检查旧信"] },
        },
      },
    });
    expect(result).toMatchObject({
      worldTitle: "联调世界", turnLabel: "第1幕", chapterTitle: "旅程开场",
      scene: ["第一段。", "第二段。"], quote: "", branches: ["前往灯塔", "检查旧信"], scenes: [],
      state: { location: "雾港", time: "", relationships: [], variables: [], echoes: [] },
    });
  });

  it("hydrates persisted character state and story variables without fixture leakage", () => {
    const result = hydrateJourneyFromSnapshot(base, {
      id: "simulation-2", worldId: "world-2", version: 3, lastTurnSeq: 2, lastSceneNo: 1, recentTurns: [], summary: "你已经答应守住灯塔。",
      worldSnapshot: { setting: { characters: [{ name: "闻汐", role: "守灯人", relation: "认得你的铜哨", public: true }] } },
      state: { location: "灯塔", tide: "退潮" },
      characters: { c1: { name: "闻汐", current: { trust: "开始信任你" } } },
      threads: { lighthouse: { key: "lighthouse", title: "重启灯塔", status: "open" } },
    });
    expect(result.state.relationships).toEqual([{ name: "闻汐", detail: "开始信任你", tone: "neutral" }]);
    expect(result.state.variables).toEqual([{ label: "tide", value: "退潮" }, { label: "进行中线索", value: "重启灯塔" }]);
    expect(result.state.echoes).toEqual(["你已经答应守住灯塔。"]);
  });

  it("derives the current scene label from scenes, not from the turn count", () => {
    const result = hydrateJourneyFromSnapshot(base, {
      id: "simulation-3", worldId: "world-3", version: 5, lastTurnSeq: 6, lastSceneNo: 3, summary: "",
      worldSnapshot: { setting: { opening: { text: "开场。", actions: ["继续"] } } },
      state: { location: "雨巷" }, characters: {}, threads: {}, recentTurns: [
        { id: "t1", simulationId: "simulation-3", seq: 1, sceneNo: 1, sceneTitle: "", sceneClosure: "", interaction: "act", userAction: "出发", proposal: { narrative: "你启程了。", choices: ["走"] }, status: "committed", createdAt: "" },
        { id: "t2", simulationId: "simulation-3", seq: 2, sceneNo: 2, sceneTitle: "雨巷", sceneClosure: "一夜之后你抵达落雨的窄巷。", interaction: "continue", userAction: "过夜", proposal: { narrative: "雨落在瓦檐上。", choices: ["敲门"] }, status: "committed", createdAt: "" },
        { id: "t3", simulationId: "simulation-3", seq: 3, sceneNo: 2, sceneTitle: "", sceneClosure: "", interaction: "act", userAction: "敲门", proposal: { narrative: "门开了。", choices: ["进去"] }, status: "committed", createdAt: "" },
        { id: "t4", simulationId: "simulation-3", seq: 4, sceneNo: 3, sceneTitle: "雨巷之约", sceneClosure: "你离开窄巷，赶到城里的钟楼赴约。", interaction: "branch", userAction: "赶往钟楼", proposal: { narrative: "你穿过半座城抵达钟楼。", choices: ["赴约"] }, status: "committed", createdAt: "" },
      ],
    });
    expect(result.turnLabel).toBe("第3幕");
    expect(result.chapterTitle).toBe("雨巷之约");
    expect(result.scenes.map((scene) => [scene.no, scene.title])).toEqual([
      [1, ""],
      [2, "雨巷"],
      [3, "雨巷之约"],
    ]);
    expect(result.scenes[1].closure).toBe("一夜之后你抵达落雨的窄巷。");
    expect(result.scenes[1].turns).toHaveLength(2);
  });
});

describe("buildScenes", () => {
  it("groups turns into scenes and orders them ascending", () => {
    const scenes = buildScenes([
      { id: "t3", simulationId: "s", seq: 3, sceneNo: 2, sceneTitle: "", sceneClosure: "", interaction: "act", userAction: "a", proposal: { narrative: "n", choices: [] } },
      { id: "t1", simulationId: "s", seq: 1, sceneNo: 1, sceneTitle: "", sceneClosure: "", interaction: "act", userAction: "a", proposal: { narrative: "n", choices: [] } },
      { id: "t2", simulationId: "s", seq: 2, sceneNo: 2, sceneTitle: "雨巷", sceneClosure: "c", interaction: "act", userAction: "a", proposal: { narrative: "n", choices: [] } },
    ]);
    expect(scenes.map((scene) => scene.no)).toEqual([1, 2]);
    expect(scenes[1].turns.map((turn) => turn.seq)).toEqual([2, 3]);
    expect(scenes[1].title).toBe("雨巷");
    expect(scenes[1].closure).toBe("c");
  });
});
