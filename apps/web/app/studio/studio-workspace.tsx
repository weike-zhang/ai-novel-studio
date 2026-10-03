"use client";

import Image from "next/image";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ApiClient, ApiError, createIdempotencyKey, type CompletedWorldDraftJob } from "@ai-novel-studio/api-client";
import { createApiClient } from "../lib/api";
import { SiteHeader } from "../lib/site-header";
import { MobileNav } from "../lib/mobile-nav";
import { Dialog } from "../lib/dialog";
import { ChevronDown } from "lucide-react";
import { processCoverFile } from "./cover-upload";
import {
  DEFAULT_DRAFT,
  REQUIRED_CONFIRMATION_FIELDS,
  SERVER_DRAFT_KEY,
  STORAGE_KEY,
  draftSetting,
  draftSource,
  draftFromAuthoringWorld,
  emptyAchievement,
  emptyCharacter,
  fieldErrorsFromIssues,
  parseServerDraftRef,
  toServerDraftRef,
  validateDraft,
  type AchievementDraft,
  type CharacterDraft,
  type Draft,
  type FieldErrors,
  type ServerDraftRef,
} from "./studio-draft";

function createStudioApi(): ApiClient {
  return createApiClient();
}

function FieldError({ id, message }: { id: string; message?: string }) {
  return message ? <p className="studio-field-error" id={id} role="alert">{message}</p> : null;
}

function AIAssistButton({ busy, onRun }: { busy: boolean; onRun: () => void }) {
  return (
    <button className="studio-ai-assist" type="button" title={busy ? "AI 正在生成…" : "AI 灵感 / 润色"} disabled={busy} onClick={onRun} aria-label="AI 灵感或润色">
      {busy ? <span className="studio-ai-spinner" aria-hidden="true" /> : <span aria-hidden="true">✦</span>}
    </button>
  );
}

function operationMessage(error: unknown, fallback: string): string {
  if (!(error instanceof ApiError)) return error instanceof Error ? error.message : fallback;
  if (error.code === "authentication_required" || error.status === 401) return "登录状态已失效，请重新登录后再试。";
  if (error.code === "version_conflict") return "这份草稿已在其他位置更新，请刷新页面后再继续。";
  if (error.code === "human_confirmation_required") return "AI 草稿已发生变化，请重新逐项确认后再发布。";
  if (error.code === "account_suspended") return "当前账户暂时不能发布，草稿仍会保留。";
  if (error.code === "publication_requires_revision") return "公开内容需要修改后才能再次提交审核。";
  if (error.code === "publication_blocked") return "这个世界不能公开传播；草稿仍保持私密。";
  if (error.code === "moderation_unavailable") return "发布审核暂时不可用，世界仍保持私密。";
  if (error.code === "network_error") return "暂时无法连接服务器，本机草稿已经保留。";
  return error.message || fallback;
}

function waitForDraftPoll(signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = window.setTimeout(resolve, 700);
    signal.addEventListener("abort", () => {
      window.clearTimeout(timer);
      resolve();
    }, { once: true });
  });
}

async function waitForWorldDraft(api: ApiClient, jobId: string, signal: AbortSignal): Promise<CompletedWorldDraftJob> {
  for (let attempt = 0; attempt < 180; attempt += 1) {
    const job = await api.getJob(jobId, { signal });
    if (job.kind !== "world_draft") throw new Error("生成任务类型不正确");
    if (job.status === "completed") return job;
    if (job.status === "failed") throw new Error("AI 没有生成可用的世界草稿");
    if (job.status === "cancelled") throw new Error("AI 起草已取消");
    await waitForDraftPoll(signal);
  }
  throw new Error("AI 起草等待超时，请稍后重试");
}

export default function StudioWorkspace() {
  const api = useMemo(() => createStudioApi(), []);
  const router = useRouter();
  const [draft, setDraft] = useState<Draft>(DEFAULT_DRAFT);
  const [, setSaveState] = useState<string>();
  const [serverDraft, setServerDraft] = useState<ServerDraftRef | null>(null);
  const [draftRevision, setDraftRevision] = useState(0);
  const [isWorking, setIsWorking] = useState(false);
  const [errors, setErrors] = useState<FieldErrors>({});
  const [isPreviewOpen, setIsPreviewOpen] = useState(false);
  const [isCharactersExpanded, setIsCharactersExpanded] = useState(false);
  const [isToneExpanded, setIsToneExpanded] = useState(false);
  const [isAdvancedExpanded, setIsAdvancedExpanded] = useState(false);
  const [isAchievementsExpanded, setIsAchievementsExpanded] = useState(false);
  const [isAiOpen, setIsAiOpen] = useState(false);
  const [aiPrompt, setAiPrompt] = useState("");
  const [reviewedAIFields, setReviewedAIFields] = useState<string[]>([]);
  const [notice, setNotice] = useState("");
  const [aiFieldBusy, setAiFieldBusy] = useState<string | null>(null);
  const loadedRef = useRef(false);
  const draftRequestRef = useRef<AbortController | null>(null);
  const draftRef = useRef(draft);
  const serverDraftRef = useRef<ServerDraftRef | null>(null);
  const savedRevisionRef = useRef(0);
  const operationQueueRef = useRef<Promise<void>>(Promise.resolve());
  const autosaveTimerRef = useRef<number | null>(null);

  useEffect(() => () => draftRequestRef.current?.abort(), []);

  useEffect(() => {
    draftRef.current = draft;
  }, [draft]);

  const rememberServerWorld = useCallback((world: Parameters<typeof toServerDraftRef>[0]) => {
    const reference = toServerDraftRef(world);
    serverDraftRef.current = reference;
    setServerDraft(reference);
    window.localStorage.setItem(SERVER_DRAFT_KEY, JSON.stringify(reference));
    return reference;
  }, []);

  const persistServerDraft = useCallback(async (snapshot: Draft, revision: number): Promise<ServerDraftRef> => {
    const current = serverDraftRef.current;
    if (current?.status !== undefined && current.status !== "draft") return current;
    if (current && savedRevisionRef.current >= revision) return current;
    if (current && current.draftSource !== draftSource(snapshot)) {
      throw new Error("当前人工草稿已经建立；全 AI 起草需要从一份新的私密草稿开始。");
    }
    let world;
    if (current) {
      try {
        world = await api.updateWorld(current.id, {
          expectedVersion: current.version,
          title: snapshot.title,
          summary: snapshot.summary,
          setting: draftSetting(snapshot),
        });
      } catch (error) {
        if (!(error instanceof ApiError) || error.status !== 404) throw error;
        serverDraftRef.current = null;
        setServerDraft(null);
        window.localStorage.removeItem(SERVER_DRAFT_KEY);
        world = await api.createWorld({
          title: snapshot.title,
          summary: snapshot.summary,
          setting: draftSetting(snapshot),
          source: draftSource(snapshot),
        });
      }
    } else {
      world = await api.createWorld({
        title: snapshot.title,
        summary: snapshot.summary,
        setting: draftSetting(snapshot),
        source: draftSource(snapshot),
      });
    }
    savedRevisionRef.current = revision;
    return rememberServerWorld(world);
  }, [api, rememberServerWorld]);

  const enqueueOperation = useCallback(<T,>(operation: () => Promise<T>): Promise<T> => {
    const result = operationQueueRef.current.then(operation, operation);
    operationQueueRef.current = result.then(() => undefined, () => undefined);
    return result;
  }, []);

  const FIELD_TO_SECTION: Record<string, number> = {
    title: 1, summary: 1, hook: 1, promise: 1, rating: 1,
    eraPlace: 3, rules: 3,
    opening: 4,
    playerBackground: 6,
  };

  function focusFirstError(nextErrors: FieldErrors) {
    const first = Object.keys(nextErrors).find((field) => nextErrors[field as keyof Draft]);
    if (!first) return;
    const sectionNo = FIELD_TO_SECTION[first];
    if (sectionNo === 2) setIsToneExpanded(true);
    if (sectionNo === 5) setIsCharactersExpanded(true);
    if (sectionNo === 6) setIsAdvancedExpanded(true);
    if (sectionNo === 7) setIsAchievementsExpanded(true);
    window.requestAnimationFrame(() => {
      document.getElementById(`studio-section-0${sectionNo}`)?.scrollIntoView({ behavior: "smooth", block: "start" });
    });
  }

  const applyOperationError = useCallback((error: unknown, fallback: string) => {
    if (error instanceof ApiError) {
      const fieldErrors = fieldErrorsFromIssues(error.body);
      if (Object.keys(fieldErrors).length > 0) setErrors((current) => ({ ...current, ...fieldErrors }));
    }
    const message = operationMessage(error, fallback);
    setSaveState(message);
    setNotice(message);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    const restoreTimer = window.setTimeout(() => {
      const saved = window.localStorage.getItem(STORAGE_KEY);
      const savedServer = parseServerDraftRef(window.localStorage.getItem(SERVER_DRAFT_KEY));
      let restoredDraft = DEFAULT_DRAFT;
      if (saved) {
        try {
          restoredDraft = { ...DEFAULT_DRAFT, ...JSON.parse(saved) as Draft };
          if (!Array.isArray(restoredDraft.characters) || restoredDraft.characters.length === 0) {
            restoredDraft.characters = [emptyCharacter()];
          }
        } catch {
          setSaveState("本机草稿无法读取，已使用示例内容");
        }
      }
      if (savedServer?.draftSource === "ai") restoredDraft = { ...restoredDraft, aiDraft: true };
      setDraft(restoredDraft);
      if (savedServer) {
        serverDraftRef.current = savedServer;
        setServerDraft(savedServer);
        setSaveState(savedServer.status === "published" ? "已公开 · 服务端版本已恢复" : savedServer.status === "pending_review" ? "审核中 · 服务端版本已恢复" : "已恢复服务器私密草稿");
        void api.getWorld(savedServer.id, { signal: controller.signal }).then((world) => {
          if (world.status !== "draft" && world.status !== "pending_review" && world.status !== "published") return;
          const refreshed: ServerDraftRef = {
            ...savedServer,
            version: world.version,
            status: world.status,
            visibility: world.visibility,
            humanConfirmedVersion: world.status === "draft" ? undefined : savedServer.humanConfirmedVersion,
          };
          serverDraftRef.current = refreshed;
          setServerDraft(refreshed);
          window.localStorage.setItem(SERVER_DRAFT_KEY, JSON.stringify(refreshed));
          setSaveState(refreshed.status === "published" ? `已公开 · 服务端版本 ${refreshed.version}` : refreshed.status === "pending_review" ? `审核中 · 服务端版本 ${refreshed.version}` : `已同步服务器私密草稿 · v${refreshed.version}`);
        }).catch((error: unknown) => {
          if (controller.signal.aborted || !(error instanceof ApiError) || error.status !== 404) return;
          serverDraftRef.current = null;
          setServerDraft(null);
          window.localStorage.removeItem(SERVER_DRAFT_KEY);
          setSaveState("原服务器草稿已不存在；继续编辑时会创建新草稿");
        });
      } else if (saved) {
        setSaveState("已从此设备恢复草稿");
      }
      loadedRef.current = true;
    }, 0);
    return () => {
      window.clearTimeout(restoreTimer);
      controller.abort();
    };
  }, [api]);

  useEffect(() => {
    if (!loadedRef.current) return;
    const timer = window.setTimeout(() => {
      window.localStorage.setItem(STORAGE_KEY, JSON.stringify(draft));
    }, 250);
    return () => window.clearTimeout(timer);
  }, [draft]);

  useEffect(() => {
    if (!loadedRef.current || draftRevision === 0 || (serverDraftRef.current && serverDraftRef.current.status !== "draft")) return;
    setSaveState("等待同步到服务器…");
    autosaveTimerRef.current = window.setTimeout(() => {
      const snapshot = draftRef.current;
      void enqueueOperation(() => persistServerDraft(snapshot, draftRevision)).then(() => {
        setSaveState(`已保存到服务器 · ${new Intl.DateTimeFormat("zh-CN", { hour: "2-digit", minute: "2-digit" }).format(new Date())}`);
      }).catch((error: unknown) => applyOperationError(error, "服务器草稿保存失败，本机草稿已经保留。"));
    }, 900);
    return () => {
      if (autosaveTimerRef.current !== null) window.clearTimeout(autosaveTimerRef.current);
    };
  }, [applyOperationError, draftRevision, enqueueOperation, persistServerDraft]);

  function updateDraft<K extends keyof Draft>(key: K, value: Draft[K]) {
    if (serverDraftRef.current && serverDraftRef.current.status !== "draft") {
      setNotice(serverDraftRef.current.status === "pending_review" ? "世界正在等待审核。撤回申请后才能继续修改。" : "世界正在公开展示。请先下架，再修改内容。");
      return;
    }
    setDraft((current) => ({
      ...current,
      [key]: value,
      aiEdited: current.aiDraft ? true : current.aiEdited,
      aiConfirmed: current.aiDraft ? false : current.aiConfirmed,
    }));
    setDraftRevision((current) => current + 1);
    if (draftRef.current.aiDraft) setReviewedAIFields([]);
    setErrors((current) => ({ ...current, [key]: undefined }));
  }

  function updateCharacter(index: number, key: keyof CharacterDraft, value: string | boolean) {
    if (serverDraftRef.current && serverDraftRef.current.status !== "draft") {
      setNotice("请先下架或撤回审核，再修改角色。");
      return;
    }
    setDraft((current) => {
      const characters = current.characters.map((character, i) =>
        i === index ? { ...character, [key]: value } : character,
      );
      return { ...current, characters };
    });
    setDraftRevision((current) => current + 1);
  }

  function addCharacter() {
    setDraft((current) => ({ ...current, characters: [...current.characters, emptyCharacter()] }));
    setDraftRevision((current) => current + 1);
  }

  function removeCharacter(index: number) {
    setDraft((current) => {
      const characters = current.characters.filter((_, i) => i !== index);
      return { ...current, characters: characters.length === 0 ? [emptyCharacter()] : characters };
    });
    setDraftRevision((current) => current + 1);
  }

  function updateAchievement(index: number, key: keyof AchievementDraft, value: string) {
    if (serverDraftRef.current && serverDraftRef.current.status !== "draft") {
      setNotice("请先下架或撤回审核，再修改世界成就。");
      return;
    }
    setDraft((current) => {
      const achievements = current.achievements.map((item, i) =>
        i === index ? { ...item, [key]: value } : item,
      );
      return { ...current, achievements };
    });
    setDraftRevision((current) => current + 1);
  }

  function addAchievement() {
    setDraft((current) => ({ ...current, achievements: [...current.achievements, emptyAchievement()] }));
    setDraftRevision((current) => current + 1);
  }

  function removeAchievement(index: number) {
    setDraft((current) => {
      const achievements = current.achievements.filter((_, i) => i !== index);
      return { ...current, achievements };
    });
    setDraftRevision((current) => current + 1);
  }

  async function createAiDraft() {
    const topic = aiPrompt.trim();
    if (!topic) {
      setNotice("请先写下主题、类型或内容边界，再生成示例草稿。");
      return;
    }
    if (serverDraftRef.current && serverDraftRef.current.status !== "draft") {
      setNotice(serverDraftRef.current.status === "pending_review" ? "请先撤回审核申请，再使用 AI 起草。" : "请先下架当前世界，再使用 AI 起草。 ");
      return;
    }
    if (serverDraftRef.current?.draftSource === "manual") {
      setNotice("当前人工草稿已经同步。全 AI 起草需要从一份新的私密草稿开始。");
      return;
    }
    draftRequestRef.current?.abort();
    const controller = new AbortController();
    draftRequestRef.current = controller;
    setIsWorking(true);
    setNotice("AI 正在根据冲突生成私密草稿…");
    try {
      const job = await api.generateWorldDraft({
        topic,
        type: draft.genre,
        contentRating: draft.rating as "全年龄" | "13+" | "16+" | "18+",
        boundaries: draft.boundaries || "不生成现实伤害指导",
      }, { idempotencyKey: createIdempotencyKey(), signal: controller.signal });
      const completed = job.status === "completed" ? job : await waitForWorldDraft(api, job.id, controller.signal);
      const generated = completed.result.world;
      setDraft({ ...draftFromAuthoringWorld(generated), visibility: "private", aiDraft: true, aiEdited: false, aiConfirmed: false });
      setReviewedAIFields([]);
      rememberServerWorld(generated);
      setSaveState(`AI 私密草稿已生成 · 版本 ${generated.version}`);
      setNotice("AI 私密草稿已生成；请先编辑，再逐项确认后公开。");
      setIsAiOpen(false);
    } catch (error) {
      if (!controller.signal.aborted) applyOperationError(error, "AI 起草失败，原有草稿仍会保留。");
    } finally {
      if (draftRequestRef.current === controller) draftRequestRef.current = null;
      setIsWorking(false);
    }
  }

  async function confirmAiDraft() {
    if (!draft.aiDraft || !draft.aiEdited || isWorking) return;
    if (reviewedAIFields.length !== REQUIRED_CONFIRMATION_FIELDS.length) {
      setNotice("请逐项检查所有发布必填内容，再提交人工确认。");
      return;
    }
    setIsWorking(true);
    if (autosaveTimerRef.current !== null) window.clearTimeout(autosaveTimerRef.current);
    try {
      const snapshot = draftRef.current;
      const reference = await enqueueOperation(async () => {
        const saved = await persistServerDraft(snapshot, draftRevision);
        const confirmed = await api.confirmAIWorldFields(saved.id, {
          expectedVersion: saved.version,
          fields: [...REQUIRED_CONFIRMATION_FIELDS],
        });
        savedRevisionRef.current = draftRevision;
        return rememberServerWorld(confirmed);
      });
      setDraft((current) => ({ ...current, aiConfirmed: true }));
      setSaveState(`AI 必填字段已确认 · 版本 ${reference.version}`);
      setNotice("AI 草稿的发布必填字段已经逐项确认；再次修改会自动使确认失效。");
    } catch (error) {
      applyOperationError(error, "AI 草稿确认失败，请稍后重试。");
    } finally {
      setIsWorking(false);
    }
  }

  function toggleAIFieldReview(field: string, checked: boolean) {
    setReviewedAIFields((current) => checked
      ? current.includes(field) ? current : [...current, field]
      : current.filter((item) => item !== field));
  }

  async function publishDraft() {
    if (isWorking) return;
    const nextErrors = validateDraft(draft);
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length > 0) {
      setNotice("还有必填内容需要补充。");
      focusFirstError(nextErrors);
      return;
    }
    if (draft.visibility !== "public") {
      setNotice("请先选择「准备公开」，再发布。");
      return;
    }
    if (draft.aiDraft && !draft.aiConfirmed) {
      setNotice("AI 草稿需要先编辑并逐项确认，之后才能发布。");
      return;
    }
    setIsWorking(true);
    if (autosaveTimerRef.current !== null) window.clearTimeout(autosaveTimerRef.current);
    try {
      const snapshot = draftRef.current;
      const published = await enqueueOperation(async () => {
        const saved = await persistServerDraft(snapshot, draftRevision);
        return api.publishWorld(saved.id, { expectedVersion: saved.version });
      });
      const reference = rememberServerWorld(published);
      setIsPreviewOpen(false);
      setSaveState(reference.status === "pending_review" ? `审核中 · 版本 ${reference.version}` : `已公开 · 版本 ${reference.version}`);
      setNotice(reference.status === "pending_review" ? "世界已提交审核，在审核通过前保持私密。" : "世界已经公开，可在世界广场被其他用户发现并开始旅程。");
    } catch (error) {
      applyOperationError(error, "世界发布失败，草稿仍保持私密。");
    } finally {
      setIsWorking(false);
    }
  }

  async function unpublishDraft() {
    const current = serverDraftRef.current;
    if (!current || current.status === "draft" || isWorking) return;
    setIsWorking(true);
    try {
      const unpublished = await enqueueOperation(() => api.unpublishWorld(current.id, { expectedVersion: current.version }));
      const reference = rememberServerWorld(unpublished);
      setDraft((value) => ({ ...value, visibility: "private", aiConfirmed: false }));
      setSaveState(`已下架并转为私密草稿 · 版本 ${reference.version}`);
      setNotice("世界已从广场下架，现在可以继续编辑；已有旅程仍保留原快照。");
    } catch (error) {
      applyOperationError(error, "世界下架失败，请稍后重试。");
    } finally {
      setIsWorking(false);
    }
  }

  async function startTestJourney() {
    if (isWorking) return;
    const nextErrors = validateDraft(draft);
    setErrors(nextErrors);
    if (Object.keys(nextErrors).length > 0) {
      setNotice("请先补齐发布必填项，再启动测试旅程。");
      focusFirstError(nextErrors);
      return;
    }
    setIsWorking(true);
    if (autosaveTimerRef.current !== null) window.clearTimeout(autosaveTimerRef.current);
    try {
      const snapshot = draftRef.current;
      const simulation = await enqueueOperation(async () => {
        const saved = await persistServerDraft(snapshot, draftRevision);
        return api.createJourney({ worldId: saved.id });
      });
      router.push(`/journey/${encodeURIComponent(simulation.id)}`);
    } catch (error) {
      applyOperationError(error, "测试旅程创建失败，草稿仍保持私密。");
      setIsWorking(false);
    }
  }

  const publicationLocked = serverDraft?.status === "published" || serverDraft?.status === "pending_review";
  const publicCharacters = draft.characters.filter((character) => character.public && character.name.trim());

  const [coverState, setCoverState] = useState<"idle" | "generating" | "uploading" | "error">("idle");

  async function generateCover() {
    if (coverState !== "idle" || isWorking) return;
    setCoverState("generating");
    try {
      const saved = await enqueueOperation(async () => {
        const snapshot = draftRef.current;
        return persistServerDraft(snapshot, draftRevision);
      });
      const result = await api.generateCover(saved.id);
      setDraft((current) => ({ ...current, cover: result.cover }));
      setDraftRevision((current) => current + 1);
      setNotice("AI 封面已生成并保存。");
    } catch (error) {
      applyOperationError(error, "AI 封面生成失败，请稍后重试。");
      setCoverState("error");
    } finally {
      setCoverState("idle");
    }
  }

  function onCoverUpload(event: React.ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    void uploadCoverFile(file);
  }

  async function uploadCoverFile(file: File) {
    if (coverState !== "idle" || isWorking) return;
    setCoverState("uploading");
    setNotice("正在检查并裁剪封面…");
    try {
      const processed = await processCoverFile(file);
      if (!processed.ok) {
        setNotice(processed.message);
        return;
      }
      const saved = await enqueueOperation(async () => {
        const snapshot = draftRef.current;
        return persistServerDraft(snapshot, draftRevision);
      });
      const result = await api.uploadCover(saved.id, processed.blob, processed.contentType);
      setDraft((current) => ({ ...current, cover: result.cover }));
      setDraftRevision((current) => current + 1);
      setNotice("封面已按 16:9 裁剪并上传。");
    } catch (error) {
      applyOperationError(error, "封面上传失败，请稍后重试。");
      setCoverState("error");
    } finally {
      setCoverState("idle");
    }
  }

  async function runAssist(field: keyof Draft & string, current: string) {
    if (aiFieldBusy) return;
    setAiFieldBusy(field);
    setNotice("");
    try {
      const result = await api.aiAssist({
        action: current.trim() ? "polish" : "inspire",
        field,
        current: current.trim(),
        contextHint: draft.title ? `世界标题：${draft.title}` : "",
      });
      updateDraft(field, result.text);
      setNotice(current.trim() ? "AI 已润色这段内容。" : "AI 灵感已填入。");
    } catch (error) {
      applyOperationError(error, "AI 暂时没有回应，请稍后重试。");
    } finally {
      setAiFieldBusy(null);
    }
  }

  return (
    <div className="site-shell">
      <SiteHeader />
      <MobileNav />

      <main className="page-container page-container--workspace studio-layout">
        <section className="studio-editor" aria-label="世界创建">
          <header className="studio-editor-heading">
            <div>
              <h1>{draft.title || "未命名世界"}</h1>
            </div>
            <button className="button secondary small" type="button" onClick={() => setIsPreviewOpen(true)}>
              预览
            </button>
          </header>

          {notice && <p className="studio-notice" role="status">{notice}</p>}

          <div className="studio-form">
            <section className="studio-subsection" id="studio-section-01">
              <div className="studio-subsection-heading"><span>01</span><h2>基本信息</h2><p className="studio-section-hint">故事的名字、面孔与基调</p></div>

              <label className="field">
                <span className="field-label">标题 <em>必填</em></span>
                <span className="studio-ai-field">
                  <input maxLength={80} value={draft.title} onChange={(event) => updateDraft("title", event.target.value)} aria-invalid={Boolean(errors.title)} />
                  <AIAssistButton busy={aiFieldBusy === "title"} onRun={() => void runAssist("title", draft.title)} />
                </span>
                <FieldError id="title-error" message={errors.title} />
              </label>

              <label className="field">
                <span className="field-label">简介 <em>必填</em></span>
                <span className="studio-ai-field">
                  <textarea placeholder="例：一场雨夜失踪案逼你在救人和守规矩之间选一个" maxLength={500} value={draft.summary} onChange={(event) => updateDraft("summary", event.target.value)} aria-invalid={Boolean(errors.summary)} />
                  <AIAssistButton busy={aiFieldBusy === "summary"} onRun={() => void runAssist("summary", draft.summary)} />
                </span>
                <FieldError id="summary-error" message={errors.summary} />
              </label>

              <div className="studio-form-row two">
                <label className="field">
                  <span className="field-label">首屏钩子</span>
                  <span className="studio-ai-field">
                    <input placeholder="例：全城每晚忘掉一个人，今晚轮到我" maxLength={80} value={draft.hook} onChange={(event) => updateDraft("hook", event.target.value)} />
                    <AIAssistButton busy={aiFieldBusy === "hook"} onRun={() => void runAssist("hook", draft.hook)} />
                  </span>
                </label>
                <label className="field">
                  <span className="field-label">玩家会做什么</span>
                  <span className="studio-ai-field">
                    <textarea placeholder="例：追回真相，也逼你拿出会伤人的选择" maxLength={120} value={draft.promise} onChange={(event) => updateDraft("promise", event.target.value)} />
                    <AIAssistButton busy={aiFieldBusy === "promise"} onRun={() => void runAssist("promise", draft.promise)} />
                  </span>
                </label>
              </div>

              <div className="studio-cover">
                <span className="field-label">封面</span>
                <div className="studio-cover-actions">
                  <label className={`button tertiary${coverState === "idle" ? "" : " disabled"}`} htmlFor="cover-upload" aria-disabled={coverState !== "idle"}>
                    <span aria-hidden="true">⤒</span> {coverState === "uploading" ? "上传中…" : "上传封面"}
                  </label>
                  <input id="cover-upload" type="file" accept="image/jpeg,image/png,image/webp" className="visually-hidden" onChange={onCoverUpload} disabled={coverState !== "idle"} />
                  <button className="button tertiary" type="button" onClick={() => void generateCover()} disabled={coverState !== "idle"}>
                    {coverState === "generating" ? "生成中…" : <><span aria-hidden="true">✦</span> AI 生成</>}
                  </button>
                  {draft.cover && (
                    <button className="button tertiary" type="button" onClick={() => updateDraft("cover", "")}>移除</button>
                  )}
                </div>
                {draft.cover ? (
                  <div className="studio-cover-preview">
                    <Image src={draft.cover} alt={`${draft.title || "世界"} 封面`} width={768} height={432} sizes="(max-width: 860px) 100vw, 768px" />
                  </div>
                ) : (
                  <p className="studio-cover-empty">还没有封面。上传一张图片，或让 AI 为你生成。</p>
                )}
              </div>

              <div className="studio-form-row three">
                <label className="field">
                  <span className="field-label">类型</span>
                  <select value={draft.genre} onChange={(event) => updateDraft("genre", event.target.value)}>
                    <option>都市奇幻</option><option>历史</option><option>科幻</option><option>情感</option><option>悬疑</option>
                  </select>
                </label>
                <label className="field">
                  <span className="field-label">内容分级 <em>必填</em></span>
                  <select value={draft.rating} onChange={(event) => updateDraft("rating", event.target.value)} aria-invalid={Boolean(errors.rating)}>
                    <option value="">请选择</option>
                    <option>全年龄</option><option>13+</option><option>16+</option><option>18+</option>
                  </select>
                  <FieldError id="rating-error" message={errors.rating} />
                </label>
                <label className="field">
                  <span className="field-label">可见范围</span>
                  <select value={draft.visibility} disabled={draft.aiDraft && !draft.aiConfirmed} onChange={(event) => updateDraft("visibility", event.target.value as "private" | "public")}>
                    <option value="private">私密</option>
                    <option value="public">准备公开</option>
                  </select>
                </label>
              </div>
            </section>

            <section className="studio-subsection studio-collapsible" id="studio-section-02">
              <button className="studio-subsection-heading" type="button" aria-expanded={isToneExpanded} onClick={() => setIsToneExpanded((value) => !value)}>
                <span>02</span><h2>叙事基调</h2><span className="studio-chevron" aria-hidden="true"><ChevronDown size={18} /></span>
              </button>
              {isToneExpanded && (
                <div className="studio-collapsible-body">
                  <div className="studio-form-row two">
                    <label className="field">
                      <span className="field-label">叙事风格</span>
                      <select value={draft.narrativeStyle} onChange={(event) => updateDraft("narrativeStyle", event.target.value)}>
                        <option>克制悬疑</option><option>轻快冒险</option><option>细腻情感</option><option>冷峻现实</option>
                      </select>
                    </label>
                    <label className="field">
                      <span className="field-label">视角</span>
                      <select value={draft.pointOfView} onChange={(event) => updateDraft("pointOfView", event.target.value)}>
                        <option>第二人称</option><option>第一人称</option><option>第三人称限知</option>
                      </select>
                    </label>
                  </div>
                </div>
              )}
            </section>

            <section className="studio-subsection" id="studio-section-03">
              <div className="studio-subsection-heading"><span>03</span><h2>世界设定</h2></div>
              <label className="field">
                <span className="field-label">时代与地点 <em>必填</em></span>
                <span className="studio-ai-field">
                  <textarea value={draft.eraPlace} onChange={(event) => updateDraft("eraPlace", event.target.value)} aria-invalid={Boolean(errors.eraPlace)} />
                  <AIAssistButton busy={aiFieldBusy === "eraPlace"} onRun={() => void runAssist("eraPlace", draft.eraPlace)} />
                </span>
                <FieldError id="era-place-error" message={errors.eraPlace} />
              </label>
              <label className="field">
                <span className="field-label">核心规则 <em>必填</em></span>
                <span className="studio-ai-field">
                  <textarea value={draft.rules} onChange={(event) => updateDraft("rules", event.target.value)} />
                  <AIAssistButton busy={aiFieldBusy === "rules"} onRun={() => void runAssist("rules", draft.rules)} />
                </span>
              </label>
            </section>

            <section className="studio-subsection" id="studio-section-04">
              <div className="studio-subsection-heading"><span>04</span><h2>开场</h2></div>
              <label className="field">
                <span className="field-label">开场文本 <em>必填</em></span>
                <span className="studio-ai-field">
                  <textarea className="opening-textarea" placeholder="例：你刚踏进灯塔，停电的那一秒，门外有人敲了三下" value={draft.opening} onChange={(event) => updateDraft("opening", event.target.value)} aria-invalid={Boolean(errors.opening)} />
                  <AIAssistButton busy={aiFieldBusy === "opening"} onRun={() => void runAssist("opening", draft.opening)} />
                </span>
                <FieldError id="opening-error" message={errors.opening} />
              </label>
              <label className="field">
                <span className="field-label">首轮建议行动</span>
                <textarea placeholder="例：1. 查失踪档案 2. 见证人 3. 追进雨巷" value={draft.actions} onChange={(event) => updateDraft("actions", event.target.value)} />
              </label>
            </section>

            <section className="studio-subsection studio-collapsible" id="studio-section-05">
              <button className="studio-subsection-heading" type="button" aria-expanded={isCharactersExpanded} onClick={() => setIsCharactersExpanded((value) => !value)}>
                <span>05</span><h2>角色 <em className="count">{draft.characters.length}</em></h2><span className="studio-chevron" aria-hidden="true"><ChevronDown size={18} /></span>
              </button>
              {isCharactersExpanded && (
                <div className="studio-collapsible-body">
                  <div className="studio-character-list">
                    {draft.characters.map((character, index) => (
                      <div className="studio-character-editor" key={index}>
                        <div className="studio-character-editor-heading">
                          <strong>{character.name || `角色 ${index + 1}`}</strong>
                          {draft.characters.length > 1 && (
                            <button className="button tertiary" type="button" onClick={() => removeCharacter(index)}>删除</button>
                          )}
                        </div>
                        <div className="studio-fields-grid">
                          <label className="field">
                            <span className="field-label">名称 <em>必填</em></span>
                            <input value={character.name} onChange={(event) => updateCharacter(index, "name", event.target.value)} />
                          </label>
                          <label className="field">
                            <span className="field-label">身份</span>
                            <input value={character.role} onChange={(event) => updateCharacter(index, "role", event.target.value)} />
                          </label>
                          <label className="field">
                            <span className="field-label">性格</span>
                            <textarea value={character.personality} onChange={(event) => updateCharacter(index, "personality", event.target.value)} />
                          </label>
                          <label className="field">
                            <span className="field-label">目标</span>
                            <textarea value={character.goal} onChange={(event) => updateCharacter(index, "goal", event.target.value)} />
                          </label>
                          <label className="field">
                            <span className="field-label">人生信条</span>
                            <textarea value={character.creed} onChange={(event) => updateCharacter(index, "creed", event.target.value)} />
                          </label>
                          <label className="field">
                            <span className="field-label">与玩家关系</span>
                            <textarea value={character.relation} onChange={(event) => updateCharacter(index, "relation", event.target.value)} />
                          </label>
                        </div>
                        <label className="studio-checkbox">
                          <input type="checkbox" checked={character.public} onChange={(event) => updateCharacter(index, "public", event.target.checked)} />
                          <span><strong>在世界详情页公开</strong></span>
                        </label>
                      </div>
                    ))}
                  </div>
                  <button className="button secondary full" type="button" onClick={addCharacter}>+ 添加角色</button>
                </div>
              )}
            </section>

            <section className="studio-subsection studio-collapsible" id="studio-section-06">
              <button className="studio-subsection-heading" type="button" aria-expanded={isAdvancedExpanded} onClick={() => setIsAdvancedExpanded((value) => !value)}>
                <span>06</span><h2>高级配置</h2><span className="studio-chevron" aria-hidden="true"><ChevronDown size={18} /></span>
              </button>
              {isAdvancedExpanded && (
                <div className="studio-collapsible-body">
                  <div className="studio-form-section">
                    <div className="studio-section-title"><span>玩家身份</span></div>
                    <div className="studio-field full"><label htmlFor="player-name">角色名称或称谓</label><input id="player-name" value={draft.playerName} onChange={(event) => updateDraft("playerName", event.target.value)} /></div>
                    <div className="studio-field full"><label htmlFor="player-background">背景 <em>必填</em></label><textarea id="player-background" value={draft.playerBackground} onChange={(event) => updateDraft("playerBackground", event.target.value)} aria-invalid={Boolean(errors.playerBackground)} /><FieldError id="player-error" message={errors.playerBackground} /></div>
                    <div className="studio-field full"><label htmlFor="player-goal">初始目标</label><textarea id="player-goal" value={draft.playerGoal} onChange={(event) => updateDraft("playerGoal", event.target.value)} /></div>
                  </div>
                  <div className="studio-form-section">
                    <div className="studio-section-title"><span>其他</span></div>
                    <div className="studio-field full"><label htmlFor="world-tags">标签</label><input id="world-tags" value={draft.tags} onChange={(event) => updateDraft("tags", event.target.value)} /></div>
                    <div className="studio-field full"><label htmlFor="world-conflict">关键冲突</label><textarea id="world-conflict" value={draft.conflict} onChange={(event) => updateDraft("conflict", event.target.value)} /></div>
                    <div className="studio-field full"><label htmlFor="world-boundaries">内容边界</label><textarea id="world-boundaries" value={draft.boundaries} onChange={(event) => updateDraft("boundaries", event.target.value)} /></div>
                    <div className="studio-field full"><label htmlFor="player-freedom">玩家自由发挥说明</label><textarea id="player-freedom" value={draft.playerFreedom} onChange={(event) => updateDraft("playerFreedom", event.target.value)} /></div>
                    <div className="studio-fields-grid">
                      <div className="studio-field"><label htmlFor="turn-length">单回合目标长度</label><select id="turn-length" value={draft.turnLength} onChange={(event) => updateDraft("turnLength", event.target.value)}><option>短（220–360 字）</option><option>中等（600–900 字）</option><option>长（1000–1400 字）</option></select></div>
                      <div className="studio-field"><label htmlFor="openness">开放程度</label><select id="openness" value={draft.openness} onChange={(event) => updateDraft("openness", event.target.value)}><option>引导明确</option><option>平衡</option><option>高开放</option></select></div>
                    </div>
                  </div>
                </div>
              )}
            </section>

            <section className="studio-subsection studio-collapsible" id="studio-section-07">
              <button className="studio-subsection-heading" type="button" aria-expanded={isAchievementsExpanded} onClick={() => setIsAchievementsExpanded((value) => !value)}>
                <span>07</span><h2>世界成就 <em className="count">{draft.achievements.length}</em></h2><span className="studio-chevron" aria-hidden="true"><ChevronDown size={18} /></span>
              </button>
              {isAchievementsExpanded && (
                <div className="studio-collapsible-body">
                  <p className="studio-hint">可选。AI 会在玩家的旅程中判断是否达成这些成就，达成即发放微光。写得越具体、越出人意料，玩家越愿意去追。同一玩家在一个世界里累计最多获得 30 微光。</p>
                  <div className="studio-achievement-list">
                    {draft.achievements.map((achievement, index) => (
                      <div className="studio-achievement-editor" key={index}>
                        <div className="studio-character-editor-heading">
                          <strong>{achievement.title || `成就 ${index + 1}`}</strong>
                          <button className="button tertiary" type="button" onClick={() => removeAchievement(index)}>删除</button>
                        </div>
                        <div className="studio-fields-grid">
                          <label className="field">
                            <span className="field-label">标识 key <em>必填</em></span>
                            <input value={achievement.key} onChange={(event) => updateAchievement(index, "key", event.target.value)} placeholder="例：answer-bell" />
                          </label>
                          <label className="field">
                            <span className="field-label">成就名 <em>必填</em></span>
                            <input value={achievement.title} onChange={(event) => updateAchievement(index, "title", event.target.value)} placeholder="例：只有钟声记得" />
                          </label>
                          <label className="field">
                            <span className="field-label">达成提示</span>
                            <textarea value={achievement.hint} onChange={(event) => updateAchievement(index, "hint", event.target.value)} placeholder="例：没有回答第二声钟时触发" />
                          </label>
                          <label className="field">
                            <span className="field-label">奖励</span>
                            <select value={achievement.reward} onChange={(event) => updateAchievement(index, "reward", event.target.value)}>
                              <option value="3">3 微光 · 较易</option>
                              <option value="6">6 微光 · 适中</option>
                              <option value="9">9 微光 · 困难</option>
                            </select>
                          </label>
                        </div>
                      </div>
                    ))}
                  </div>
                  <button className="button secondary full" type="button" onClick={addAchievement}>+ 添加成就</button>
                </div>
              )}
            </section>
          </div>

          {draft.aiDraft && !draft.aiConfirmed && (
            <section className="studio-subsection studio-ai-review" aria-label="AI 草稿人工确认清单">
              <div className="studio-subsection-heading"><span>★</span><h2>逐项检查后确认</h2><p className="studio-section-hint">AI 结果不能直接公开，必须由你逐项确认</p></div>
              {[
                ["title", "标题"],
                ["summary", "简介"],
                ["setting.worldBackground", "世界背景与规则"],
                ["setting.playerIdentity", "玩家身份"],
                ["setting.characters", "角色核心身份"],
                ["setting.opening", "开场"],
                ["setting.contentRating", "内容分级"],
              ].map(([field, label]) => (
                <label key={field}>
                  <input
                    type="checkbox"
                    checked={reviewedAIFields.includes(field)}
                    disabled={isWorking}
                    onChange={(event) => toggleAIFieldReview(field, event.target.checked)}
                  />
                  <span>{label}</span>
                </label>
              ))}
              <button
                className="button accent"
                type="button"
                disabled={!draft.aiEdited || isWorking || reviewedAIFields.length !== REQUIRED_CONFIRMATION_FIELDS.length}
                onClick={() => void confirmAiDraft()}
              >提交人工确认</button>
            </section>
          )}

          <div className="studio-primary-actions">
            <div className="studio-primary-buttons">
              <button className="button secondary" type="button" onClick={() => void startTestJourney()} disabled={isWorking}>
                测试旅程
              </button>
              {publicationLocked
                ? <button className="button secondary" type="button" disabled={isWorking} onClick={() => void unpublishDraft()}>{serverDraft?.status === "pending_review" ? "撤回审核" : "下架"}</button>
                : <button className="button accent" type="button" disabled={isWorking} onClick={() => void publishDraft()}>{isWorking ? "正在发布…" : "公开发布"}</button>}
            </div>
          </div>
        </section>
      </main>


      {isAiOpen && (
        <Dialog title="AI 起草" onClose={() => setIsAiOpen(false)}>
          <p className="studio-hint">写下主题、类型和边界。AI 会先生成私密草稿，由你确认每一项设定后才能公开。</p>
          <label className="field">
            <span className="field-label">创作想法</span>
            <textarea value={aiPrompt} onChange={(event) => setAiPrompt(event.target.value)} placeholder="例：潮湿海港中的信件悬疑；不要现实伤害指导" />
          </label>
          <button className="button primary full" type="button" disabled={isWorking} onClick={() => void createAiDraft()}>{isWorking ? "生成中…" : "生成私密草稿"}</button>
          <p className="studio-hint">AI 结果不能直接公开，必须由你确认和编辑。</p>
        </Dialog>
      )}

      {isPreviewOpen && (
        <Dialog title={draft.title} onClose={() => setIsPreviewOpen(false)}>
          <p className="studio-preview-meta">{draft.genre} · {draft.rating}</p>
          <p className="studio-preview-summary">{draft.summary}</p>
          {draft.hook && <p className="studio-preview-hook">{draft.hook}</p>}
          {draft.promise && <p className="studio-preview-promise">{draft.promise}</p>}
          <section className="studio-preview-block">
            <h3>世界背景</h3>
            <p>{draft.eraPlace} {draft.rules}</p>
          </section>
          <section className="studio-preview-block">
            <h3>你在这里是谁</h3>
            <p>{draft.playerBackground}</p>
          </section>
          <section className="studio-preview-block">
            <h3>公开角色</h3>
            {publicCharacters.length > 0
              ? <p>{publicCharacters.map((character) => `${character.name} · ${character.role}。${character.personality}`).join(" ")}</p>
              : <p>暂未设置公开角色。</p>}
          </section>
          <section className="studio-preview-block">
            <h3>开场</h3>
            <p>{draft.opening}</p>
          </section>
          <footer className="studio-detail-footer">
            <button className="button secondary" type="button" onClick={() => setIsPreviewOpen(false)}>返回编辑</button>
            {!publicationLocked && <button className="button accent" type="button" disabled={isWorking} onClick={() => void publishDraft()}>{isWorking ? "正在发布…" : "公开发布"}</button>}
          </footer>
        </Dialog>
      )}
    </div>
  );
}
