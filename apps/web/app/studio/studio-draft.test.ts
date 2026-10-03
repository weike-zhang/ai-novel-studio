import { describe, expect, it } from "vitest";
import {
  DEFAULT_DRAFT,
  draftFromAuthoringWorld,
  draftSetting,
  draftSource,
  fieldErrorsFromIssues,
  parseServerDraftRef,
  validateDraft,
} from "./studio-draft";

describe("studio draft contract", () => {
  it("maps all publishable fields to the server setting schema", () => {
    const setting = draftSetting(DEFAULT_DRAFT);
    expect(setting).toMatchObject({
      type: "都市奇幻",
      tags: ["悬疑", "慢热探索"],
      worldBackground: { eraPlace: DEFAULT_DRAFT.eraPlace, rules: DEFAULT_DRAFT.rules },
      playerIdentity: { background: DEFAULT_DRAFT.playerBackground },
      characters: [{ name: DEFAULT_DRAFT.characters[0].name }],
      opening: { text: DEFAULT_DRAFT.opening, actions: DEFAULT_DRAFT.actions.split("\n") },
      contentRating: "16+",
    });
    expect(validateDraft(DEFAULT_DRAFT)).toEqual({});
  });

  it("keeps AI origin after human review", () => {
    expect(draftSource({ ...DEFAULT_DRAFT, aiDraft: true, aiEdited: true, aiConfirmed: true })).toBe("ai");
  });

  it("restores a complete AI world from the server-owned snapshot", () => {
    const restored = draftFromAuthoringWorld({
      id: "world-1",
      ownerId: "owner-1",
      title: "潮声档案",
      summary: "只有退潮时才会出现的旧城区。",
      setting: {
        type: "悬疑",
        tags: ["海港", "调查"],
        cover: "界门 · 雾石",
        worldBackground: { eraPlace: "架空港城", rules: "潮退后进入", conflict: "旧案重现" },
        playerIdentity: { name: "调查员", background: "受托查案", goal: "找到失踪者", freedom: "可拒绝委托" },
        characters: [{ name: "林岚", role: "领航员", personality: "冷静", goal: "守住秘密", relation: "向导", public: true }],
        narrative: { style: "克制悬疑", pointOfView: "第二人称", turnLength: "中等（600–900 字）", openness: "高开放" },
        opening: { text: "潮水退去，石阶露出。", actions: ["进入旧城", "询问领航员"] },
        contentRating: "16+",
        contentBoundaries: "不生成现实伤害指导",
      },
      visibility: "private",
      status: "draft",
      version: 2,
      draftSource: "ai",
      humanConfirmedVersion: 2,
      createdAt: "2026-08-15T00:00:00Z",
      updatedAt: "2026-08-15T00:00:00Z",
    });
    expect(restored).toMatchObject({
      title: "潮声档案",
      genre: "悬疑",
      tags: "海港、调查",
      actions: "进入旧城\n询问领航员",
      aiDraft: true,
      aiEdited: true,
      aiConfirmed: true,
      visibility: "private",
    });
    expect(restored.characters[0].name).toBe("林岚");
    expect(restored.characters[0].creed).toBe("");
  });

  it("maps backend publication issues to visible fields", () => {
    expect(fieldErrorsFromIssues({ issues: [
      { field: "summary", message: "发布前必须填写简介" },
      { field: "setting.characters", message: "发布前至少需要一名角色" },
    ] })).toEqual({ summary: "发布前必须填写简介", title: "发布前至少需要一名角色" });
  });

  it("only restores complete server draft references", () => {
    const stored = JSON.stringify({ id: "world-1", version: 3, draftSource: "ai", status: "draft", visibility: "private" });
    expect(parseServerDraftRef(stored)).toMatchObject({ id: "world-1", version: 3, draftSource: "ai" });
    expect(parseServerDraftRef(JSON.stringify({ id: "world-1", version: 4, draftSource: "manual", status: "pending_review", visibility: "private" }))).toMatchObject({ status: "pending_review", version: 4 });
    expect(parseServerDraftRef('{"id":"world-1"}')).toBeNull();
    expect(parseServerDraftRef("not-json")).toBeNull();
  });
});
