import type { AuthoringWorld, DraftSource } from "@ai-novel-studio/api-client";

export type CharacterDraft = {
  name: string;
  role: string;
  personality: string;
  goal: string;
  creed: string;
  relation: string;
  public: boolean;
};

export type AchievementDraft = {
  key: string;
  title: string;
  hint: string;
  reward: string;
};

export type Draft = {
  title: string;
  summary: string;
  hook: string;
  promise: string;
  playTime: string;
  format: string;
  genre: string;
  tags: string;
  coverStyle: string;
  cover: string;
  rating: string;
  eraPlace: string;
  rules: string;
  conflict: string;
  boundaries: string;
  playerName: string;
  playerBackground: string;
  playerGoal: string;
  playerFreedom: string;
  characters: CharacterDraft[];
  achievements: AchievementDraft[];
  narrativeStyle: string;
  pointOfView: string;
  turnLength: string;
  openness: string;
  opening: string;
  actions: string;
  visibility: "private" | "public";
  aiDraft: boolean;
  aiEdited: boolean;
  aiConfirmed: boolean;
};

export type FieldErrors = Partial<Record<keyof Draft, string>>;

export type ServerDraftRef = Pick<
  AuthoringWorld,
  "id" | "version" | "draftSource" | "status" | "visibility" | "humanConfirmedVersion"
>;

export const STORAGE_KEY = "novelstudio-studio-draft-v1";
export const SERVER_DRAFT_KEY = "novelstudio-studio-server-draft-v1";
export const REQUIRED_CONFIRMATION_FIELDS = [
  "title",
  "summary",
  "setting.worldBackground",
  "setting.playerIdentity",
  "setting.characters",
  "setting.opening",
  "setting.contentRating",
] as const;

export function emptyCharacter(): CharacterDraft {
  return { name: "", role: "", personality: "", goal: "", creed: "", relation: "", public: true };
}

export function emptyAchievement(): AchievementDraft {
  return { key: "", title: "", hint: "", reward: "6" };
}

export const DEFAULT_DRAFT: Draft = {
  title: "雾港来信",
  summary: "最后一班渡船离港后，雾里响起一口没有指针的潮钟。你必须在黎明前找回它，否则这座港城会忘掉一个人。",
  hook: "一座会遗忘人的港城，今晚轮到谁消失？",
  promise: "追踪潮雾里的旧记忆，在相信守灯人和追问真相之间作出选择。",
  playTime: "首幕约 3 分钟",
  format: "短回合悬疑",
  genre: "都市奇幻",
  tags: "悬疑、慢热探索",
  coverStyle: "界门 · 深炭",
  cover: "",
  rating: "16+",
  eraPlace: "近现代架空海港；街道随夜间潮汐钟重排。",
  rules: "潮雾只能显现真实发生却被人遗忘的片段；第二声钟响后不能回答雾中的声音。",
  conflict: "灯塔将在黎明前熄灭，写着玩家名字的旧信同时出现在第七码头。",
  boundaries: "不生成现实伤害指导；避免将失踪与哀伤浪漫化。",
  playerName: "夜班邮差",
  playerBackground: "临时接替夜班的无名邮差，随身携带旧铜哨和浸水路线图。",
  playerGoal: "在天亮前送出第一封无法辨认地址的信。",
  playerFreedom: "玩家可自行决定来历、立场，以及是否相信信件内容。",
  characters: [
    {
      name: "闻汐",
      role: "守灯人",
      personality: "沉静、谨慎，对雾的变化异常敏锐。",
      goal: "找到失踪的上一任守灯人。",
      creed: "灯不能熄，尤其是在你最想让它熄掉的时候。",
      relation: "她似乎认得玩家随身的铜哨。",
      public: true,
    },
  ],
  achievements: [],
  narrativeStyle: "克制悬疑",
  pointOfView: "第二人称",
  turnLength: "短（220–360 字）",
  openness: "高开放",
  opening: "最后一班渡船离港后，码头的雾突然漫过鞋面。你听见一口没有指针的潮钟在雾里响了一次，空白航图上随即浮出一条不属于任何港口的线。\n\n守灯人闻汐从灯影里走出来，铜哨在她掌心发冷。她没有问你从哪里来，只盯着那条新出现的航线：‘如果你看见第七码头的第二声钟，别回答。’远处，第二声钟已经悬在雾里。",
  actions: "追问闻汐为何不能回答\n沿着新航线走进潮雾\n先守住码头上的最后一盏灯",
  visibility: "private",
  aiDraft: false,
  aiEdited: false,
  aiConfirmed: false,
};

export function validateDraft(draft: Draft): FieldErrors {
  const errors: FieldErrors = {};
  if (!draft.title.trim()) errors.title = "发布前需要填写标题。";
  if (draft.title.length > 80) errors.title = "标题不能超过 80 个字。";
  if (!draft.summary.trim()) errors.summary = "发布前需要填写简介。";
  if (draft.summary.length > 500) errors.summary = "简介不能超过 500 个字。";
  if (!draft.eraPlace.trim() || !draft.rules.trim()) errors.eraPlace = "请补充时代地点与核心规则。";
  if (!draft.playerBackground.trim()) errors.playerBackground = "请说明玩家身份。";
  if (!draft.characters.some((character) => character.name.trim())) errors.title = "至少需要 1 名有名称的角色。";
  if (!draft.opening.trim()) errors.opening = "请填写开场文本或场景描述。";
  if (!draft.rating) errors.rating = "请选择内容分级。";
  return errors;
}

export function draftSetting(draft: Draft): Record<string, unknown> {
  return {
    type: draft.genre,
    tags: draft.tags.split(/[、,]/).map((tag) => tag.trim()).filter(Boolean),
    cover: draft.cover || draft.coverStyle,
    discovery: { hook: draft.hook, promise: draft.promise, playTime: draft.playTime, format: draft.format },
    worldBackground: { eraPlace: draft.eraPlace, rules: draft.rules, conflict: draft.conflict },
    playerIdentity: { name: draft.playerName, background: draft.playerBackground, goal: draft.playerGoal, freedom: draft.playerFreedom },
    characters: draft.characters
      .filter((character) => character.name.trim())
      .map((character) => ({
        name: character.name,
        role: character.role,
        personality: character.personality,
        goal: character.goal,
        creed: character.creed,
        relation: character.relation,
        public: character.public,
      })),
    narrative: { style: draft.narrativeStyle, pointOfView: draft.pointOfView, turnLength: draft.turnLength, openness: draft.openness },
    opening: { text: draft.opening, actions: draft.actions.split("\n").map((action) => action.trim()).filter(Boolean) },
    achievements: draft.achievements
      .filter((item) => item.key.trim() && item.title.trim())
      .map((item) => ({
        key: item.key.trim(),
        title: item.title.trim(),
        hint: item.hint.trim(),
        reward: parseInt(item.reward, 10) || 3,
      })),
    contentRating: draft.rating,
    contentBoundaries: draft.boundaries,
  };
}

function record(value: unknown): Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

function text(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function textList(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : [];
}

/** Converts the server-owned world snapshot into the editor's unsaved buffer. */
export function draftFromAuthoringWorld(world: AuthoringWorld): Draft {
  const setting = record(world.setting);
  const background = record(setting.worldBackground);
  const player = record(setting.playerIdentity);
  const narrative = record(setting.narrative);
  const opening = record(setting.opening);
  const discovery = record(setting.discovery);
  const rawCharacters = Array.isArray(setting.characters) ? setting.characters.map(record) : [];
  const characters: CharacterDraft[] = rawCharacters.map((character) => ({
    name: text(character.name),
    role: text(character.role),
    personality: text(character.personality),
    goal: text(character.goal),
    creed: text(character.creed),
    relation: text(character.relation),
    public: typeof character.public === "boolean" ? character.public : false,
  }));
  if (characters.length === 0) characters.push(emptyCharacter());
  const rawAchievements = Array.isArray(setting.achievements) ? setting.achievements.map(record) : [];
  const achievements: AchievementDraft[] = rawAchievements
    .filter((item) => Boolean(text(item.key) && text(item.title)))
    .map((item) => ({
      key: text(item.key) ?? "",
      title: text(item.title) ?? "",
      hint: text(item.hint) ?? "",
      reward: typeof item.reward === "number" && [3, 6, 9].includes(item.reward) ? String(item.reward) : "3",
    }));
  const sourceIsAI = world.draftSource === "ai";
  const confirmed = sourceIsAI && world.humanConfirmedVersion === world.version;

  return {
    ...DEFAULT_DRAFT,
    title: world.title,
    summary: world.summary,
    hook: text(discovery.hook),
    promise: text(discovery.promise),
    playTime: text(discovery.playTime),
    format: text(discovery.format),
    genre: text(setting.type),
    tags: textList(setting.tags).join("、"),
    coverStyle: text(setting.cover),
    rating: text(setting.contentRating),
    eraPlace: text(background.eraPlace),
    rules: text(background.rules),
    conflict: text(background.conflict),
    boundaries: text(setting.contentBoundaries),
    playerName: text(player.name),
    playerBackground: text(player.background),
    playerGoal: text(player.goal),
    playerFreedom: text(player.freedom),
    characters,
    achievements,
    narrativeStyle: text(narrative.style),
    pointOfView: text(narrative.pointOfView),
    turnLength: text(narrative.turnLength),
    openness: text(narrative.openness),
    opening: text(opening.text),
    actions: textList(opening.actions).join("\n"),
    visibility: world.visibility,
    aiDraft: sourceIsAI,
    aiEdited: sourceIsAI && world.version > 1,
    aiConfirmed: confirmed,
  };
}

export function draftSource(draft: Draft): DraftSource {
  return draft.aiDraft ? "ai" : "manual";
}

export function toServerDraftRef(world: AuthoringWorld): ServerDraftRef {
  return {
    id: world.id,
    version: world.version,
    draftSource: world.draftSource,
    status: world.status,
    visibility: world.visibility,
    humanConfirmedVersion: world.humanConfirmedVersion,
  };
}

export function parseServerDraftRef(value: string | null): ServerDraftRef | null {
  if (!value) return null;
  try {
    const parsed = JSON.parse(value) as Partial<ServerDraftRef>;
    if (
      typeof parsed.id !== "string" ||
      typeof parsed.version !== "number" ||
      (parsed.draftSource !== "manual" && parsed.draftSource !== "ai") ||
      (parsed.status !== "draft" && parsed.status !== "pending_review" && parsed.status !== "published") ||
      (parsed.visibility !== "private" && parsed.visibility !== "public")
    ) return null;
    return parsed as ServerDraftRef;
  } catch {
    return null;
  }
}

export function fieldErrorsFromIssues(value: unknown): FieldErrors {
  if (typeof value !== "object" || value === null || !("issues" in value) || !Array.isArray(value.issues)) return {};
  const fieldMap: Record<string, keyof Draft> = {
    title: "title",
    summary: "summary",
    "setting.worldBackground": "eraPlace",
    "setting.playerIdentity": "playerBackground",
    "setting.characters": "title",
    "setting.opening": "opening",
    "setting.contentRating": "rating",
  };
  const errors: FieldErrors = {};
  for (const issue of value.issues) {
    if (typeof issue !== "object" || issue === null) continue;
    const field = "field" in issue && typeof issue.field === "string" ? fieldMap[issue.field] : undefined;
    const message = "message" in issue && typeof issue.message === "string" ? issue.message : undefined;
    if (field && message) errors[field] = message;
  }
  return errors;
}
