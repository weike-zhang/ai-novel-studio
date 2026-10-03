"use client";

import { FormEvent, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import Image from "next/image";
import { ApiClient, ApiError, createIdempotencyKey, type TurnInteraction } from "@ai-novel-studio/api-client";
import { createApiClient } from "../lib/api";
import { Dialog } from "../lib/dialog";
import {
  clearActiveJob,
  type AchievementAward,
  type CommittedTurn,
  isJobStatus,
  parseTurnJobSnapshot,
  restoreActiveJob,
  saveActiveJob,
  toJourneyViewStatus,
  type TurnJobSnapshot,
} from "./job-state";
import { buildScenes, hydrateJourneyFromSnapshot, type JourneyData, type JourneyScene } from "./journey-data";
import { splitStoryParagraphs } from "./journey-text";
import { LOGO_STATE_COPY, LogoStatePanel } from "../lib/logo-state-mark";
import { MobileNav } from "../lib/mobile-nav";
import { SiteHeader } from "../lib/site-header";
import { ChevronDown, ChevronLeft, RefreshCw } from "lucide-react";

type JourneyStatus =
  | "ready"
  | "queued"
  | "generating"
  | "saving"
  | "failed"
  | "stopped";

const workingStatuses: JourneyStatus[] = ["queued", "generating", "saving"];

const pollIntervalMs = 700;
const fallbackPollIntervalMs = 2000;

function createJourneyApi(): ApiClient {
  return createApiClient();
}

function waitForNextPoll(signal: AbortSignal, intervalMs: number): Promise<void> {
  return new Promise((resolve) => {
    const timeout = window.setTimeout(resolve, intervalMs);
    signal.addEventListener("abort", () => {
      window.clearTimeout(timeout);
      resolve();
    }, { once: true });
  });
}

function userFacingError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.code === "insufficient_credits" || error.status === 402) {
      return "当前微光不足，本轮没有提交，也不会扣除微光。补充微光后可以重新尝试。";
    }
    if (error.status === 0) return "暂时无法连接故事服务。你的既有内容仍已保存，可以稍后重试。";
    if (error.code === "conflict") return "已有一幕正在生成，正在尝试恢复它。";
    return error.message || "这一幕没有完成，微光会自动退回。";
  }
  return "这一幕没有完成，微光会自动退回。";
}

function generationFailureMessage(errorCode?: string): string {
  switch (errorCode) {
    case "plan_rejected":
    case "plan_rejected_unknown_character":
    case "plan_rejected_character_precondition_required":
      return "这一幕的角色状态还没有准备好，微光已退回。可以重试。";
    case "plan_rejected_thread_not_open":
    case "plan_rejected_thread_already_exists":
      return "这一幕的剧情线索发生冲突，微光已退回。可以重试。";
    case "proposal_rejected":
      return "这一幕的正文与故事状态不一致，微光已退回。可以重试。";
    default:
      return errorCode
        ? `这一幕没有完成（${errorCode}），微光已退回。可以重试。`
        : "这一幕没有完成，微光已退回。可以重试。";
  }
}

function StatePanel({ journey, idPrefix }: { journey: JourneyData; idPrefix: string }) {
  const location = journey.state.location.trim();
  const time = journey.state.time.trim();
  return (
    <div className="state-panel-content">
      {(location || time) && (
        <section className="state-section" aria-labelledby={`${idPrefix}-place-title`}>
          <p className="state-kicker" id={`${idPrefix}-place-title`}>地点</p>
          {location && <strong>{location}</strong>}
          {time && <span>{time}</span>}
        </section>
      )}

      <section className="state-section" aria-labelledby={`${idPrefix}-thread-title`}>
        <p className="state-kicker" id={`${idPrefix}-thread-title`}>线索</p>
        {journey.state.variables.filter((item) => item.label === "进行中线索").length > 0 ? (
          <ul className="echo-list">
            {journey.state.variables.filter((item) => item.label === "进行中线索").map((item, index) => <li key={index}>{item.value}</li>)}
          </ul>
        ) : <p className="state-empty">暂时没有进行中的线索。</p>}
      </section>

      <details className="state-section state-collapsible">
        <summary>人物关系与回响</summary>
        <div className="state-collapsible-body">
          <section aria-label="人物关系">
            <ul className="relationship-list">
              {journey.state.relationships.map((item) => (
                <li key={item.name}>
                  <span className={`relation-mark ${item.tone}`} aria-hidden="true" />
                  <div><strong>{item.name}</strong><span>{item.detail}</span></div>
                </li>
              ))}
            </ul>
          </section>
          <section aria-label="回响">
            <ul className="echo-list">
              {journey.state.echoes.map((echo) => <li key={echo}>{echo}</li>)}
            </ul>
          </section>
        </div>
      </details>
    </div>
  );
}

function TurnBlock({ turn }: { turn: CommittedTurn }) {
  const paragraphs = splitStoryParagraphs(turn.proposal.narrative);
  return (
    <div className="journey-turn" aria-live="polite">
      {paragraphs.map((paragraph, index) => (
        <p key={`${turn.seq}-${index}`} className="story-para">{paragraph}</p>
      ))}
    </div>
  );
}

function SceneHeading({ scene }: { scene: JourneyScene }) {
  return (
    <header className="journey-scene-heading">
      <p className="chapter-index">第 {scene.no} 幕</p>
      <h3>{scene.title}</h3>
      {scene.closure && <p className="journey-scene-closure">{scene.closure}</p>}
    </header>
  );
}

function SceneToc({ scenes, onJump }: { scenes: JourneyScene[]; onJump?: () => void }) {
  const jumpTo = (no: number) => {
    document.getElementById(`scene-${no}`)?.scrollIntoView({ behavior: "smooth", block: "start" });
    onJump?.();
  };
  return (
    <nav className="journey-toc" aria-label="幕次目录">
      {scenes.length === 0 ? (
        <p className="state-empty">故事尚未展开，完成第一幕后这里会列出每一幕。</p>
      ) : (
        <ol>
          {scenes.map((scene) => (
            <li key={scene.no}>
              <button type="button" onClick={() => jumpTo(scene.no)}>
                <span>第 {scene.no} 幕</span>
                <strong>{scene.title}</strong>
              </button>
            </li>
          ))}
        </ol>
      )}
    </nav>
  );
}

function AchievementBar({ awards, onClose }: { awards: AchievementAward[]; onClose: () => void }) {
  return (
    <div className="achievement-bar" role="status" aria-live="polite">
      {awards.map((award) => (
        <div className="achievement-bar-item" key={`${award.scope}-${award.code}`}>
          <span className="achievement-mark" aria-hidden="true"><Image src="/brand/novelstudio-mark.svg" alt="" width={20} height={20} /></span>
          <div>
            <strong>{award.title}</strong>
            <p>{award.description}</p>
          </div>
          {award.reward > 0 && <span className="achievement-reward">+{award.reward} 微光</span>}
        </div>
      ))}
      <button type="button" onClick={onClose} aria-label="关闭成就提示">×</button>
    </div>
  );
}

export default function JourneyExperience({ journey }: { journey: JourneyData }) {
  const [currentJourney, setCurrentJourney] = useState(journey);
  const [history, setHistory] = useState<CommittedTurn[]>([]);
  const [draft, setDraft] = useState("");
  const [status, setStatus] = useState<JourneyStatus>("ready");
  const [selectedBranch, setSelectedBranch] = useState("");
  const [forksOpen, setForksOpen] = useState(false);
  const [generatedTurnId, setGeneratedTurnId] = useState("");
  const [unlockNotice, setUnlockNotice] = useState<AchievementAward[]>([]);
  const [activeJobId, setActiveJobId] = useState<string | null>(null);
  const [failedJobId, setFailedJobId] = useState<string | null>(null);
  const [pendingAction, setPendingAction] = useState("");
  const [errorMessage, setErrorMessage] = useState<string | null>(null);
  const [entering, setEntering] = useState(true);
  const [stateOpen, setStateOpen] = useState(false);
  const [lastRealCost, setLastRealCost] = useState<number | null>(null);
  const api = useMemo(createJourneyApi, []);
  const baseJourneyRef = useRef(journey);
  const pendingActionRef = useRef("");
  const requestRef = useRef<AbortController | null>(null);
  const unlockTimerRef = useRef<number | null>(null);
  const submittingRef = useRef(false);
  const balanceBeforeRef = useRef<number | null>(null);

  const isWorking = workingStatuses.includes(status);
  const scenes = useMemo(() => buildScenes(history), [history]);
  const latestTurn = history.at(-1);
  const hasForks = (latestTurn?.proposal.choices.length ?? 0) > 0;
  const statusLine = !latestTurn
    ? "旅程即将开始"
    : !hasForks
      ? "世界不可干预 · 剧情自行推进"
      : latestTurn.interaction === "continue"
        ? "你选择继续 · 世界沿显而易见的路径推进"
        : "可以干预 · 岔路已备好";

  useEffect(() => () => {
    requestRef.current?.abort();
    if (unlockTimerRef.current !== null) window.clearTimeout(unlockTimerRef.current);
  }, []);

  useEffect(() => {
    if (!entering) return;
    const key = `portal:${journey.simulationId}`;
    let alreadySeen = false;
    try {
      alreadySeen = Boolean(window.sessionStorage.getItem(key));
    } catch {
      alreadySeen = false;
    }
    const timer = window.setTimeout(() => {
      try {
        if (window.sessionStorage.getItem(key)) {
          setEntering(false);
          return;
        }
        window.sessionStorage.setItem(key, "1");
      } catch {
        return;
      }
      setEntering(false);
    }, alreadySeen ? 0 : 800);
    return () => window.clearTimeout(timer);
  }, [entering, journey.simulationId]);

  useEffect(() => {
    const controller = new AbortController();
    void api.getSimulation(journey.simulationId, { signal: controller.signal }).then((snapshot) => {
      const hydrated = hydrateJourneyFromSnapshot(baseJourneyRef.current, snapshot);
      setCurrentJourney(hydrated);
      const turns = [...snapshot.recentTurns].sort((a, b) => a.seq - b.seq);
      setHistory(turns);
      const newest = turns.at(-1);
      if (newest?.achievements?.length) {
        showUnlockNotice(newest.achievements);
      }
    }).catch((error: unknown) => {
      if (!controller.signal.aborted) setErrorMessage(userFacingError(error));
    });
    return () => controller.abort();
  }, [api, journey.simulationId]);

  useEffect(() => {
    let reference;
    try {
      reference = restoreActiveJob(window.sessionStorage, journey.simulationId);
    } catch {
      reference = null;
    }
    if (!reference) return;
    pendingActionRef.current = reference.action ?? "";
    const controller = new AbortController();
    requestRef.current = controller;
    void followJob(reference.jobId, controller);
    return () => controller.abort();
    // Restoring is intentionally tied to the journey identity, not render state.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [journey.simulationId]);

  function showUnlockNotice(awards: AchievementAward[]) {
    setUnlockNotice(awards);
    if (unlockTimerRef.current !== null) window.clearTimeout(unlockTimerRef.current);
    unlockTimerRef.current = window.setTimeout(() => setUnlockNotice([]), 6000);
  }

  function safelySaveJob(jobId: string, action = pendingAction) {
    try {
      saveActiveJob(window.sessionStorage, { simulationId: journey.simulationId, jobId, action });
    } catch {
      // Session recovery is best-effort; job execution remains durable server-side.
    }
  }

  function safelyClearJob() {
    try {
      clearActiveJob(window.sessionStorage);
    } catch {
      // Storage may be unavailable in hardened browser modes.
    }
  }

  function appendCommittedTurn(turn: CommittedTurn) {
    setHistory((current) => (current.some((item) => item.id === turn.id) ? current : [...current, turn]));
    setGeneratedTurnId(turn.id);
    if (turn.achievements?.length) showUnlockNotice(turn.achievements);
  }

  function applySnapshot(snapshot: TurnJobSnapshot): boolean {
    setStatus(toJourneyViewStatus(snapshot.status));
    if (snapshot.status === "completed") {
      if (!snapshot.committedTurn) {
        setStatus("failed");
        setErrorMessage("这一幕已经完成，但正文暂时无法读取。请重新载入页面恢复。");
        return true;
      }
      appendCommittedTurn(snapshot.committedTurn);
      setDraft("");
      setSelectedBranch("");
      setForksOpen(false);
      setPendingAction("");
      pendingActionRef.current = "";
      setFailedJobId(null);
      setActiveJobId(null);
      setErrorMessage(null);
      submittingRef.current = false;
      if (balanceBeforeRef.current !== null) {
        void api.getMe().then((me) => {
          setLastRealCost(Math.max(0, me.availableBalance - (balanceBeforeRef.current ?? 0)));
        }).catch(() => setLastRealCost(null));
      } else {
        setLastRealCost(null);
      }
      safelyClearJob();
      return true;
    }
    if (snapshot.status === "failed") {
      setFailedJobId(snapshot.id);
      setActiveJobId(null);
      submittingRef.current = false;
      setErrorMessage(generationFailureMessage(snapshot.errorCode));
      return true;
    }
    if (snapshot.status === "cancelled") {
      setFailedJobId(null);
      setActiveJobId(null);
      submittingRef.current = false;
      setErrorMessage(null);
      pendingActionRef.current = "";
      safelyClearJob();
      return true;
    }
    setActiveJobId(snapshot.id);
    return false;
  }

  async function pollJob(jobId: string, controller: AbortController, intervalMs = pollIntervalMs) {
    while (!controller.signal.aborted) {
      try {
        const snapshot = parseTurnJobSnapshot(await api.getJob(jobId, { signal: controller.signal }));
        if (!snapshot) throw new Error("Invalid turn job response");
        if (applySnapshot(snapshot)) return;
        await waitForNextPoll(controller.signal, intervalMs);
      } catch (error) {
        if (controller.signal.aborted) return;
        setStatus("failed");
        setActiveJobId(null);
        setFailedJobId(jobId);
        setErrorMessage(userFacingError(error));
        return;
      }
    }
  }

  async function followJob(jobId: string, controller: AbortController) {
    try {
      await api.streamJobEvents(jobId, (event) => {
        if (controller.signal.aborted) return;
        const snapshot = jobSnapshotFromEvent(event, jobId, journey.simulationId);
        if (snapshot && applySnapshot(snapshot)) {
          controller.abort();
        }
      }, { signal: controller.signal });
    } catch {
      if (controller.signal.aborted) return;
      // Streams can be unavailable (proxies, hardened browsers) or drop.
      // Degrade to the slower recovery poll per the design contract.
      await pollJob(jobId, controller, fallbackPollIntervalMs);
      return;
    }
    // Stream ended without a terminal event (e.g. server timeout): catch up by polling.
    if (!controller.signal.aborted) await pollJob(jobId, controller);
  }

  function jobSnapshotFromEvent(event: unknown, jobId: string, simulationId: string): TurnJobSnapshot | null {
    if (!event || typeof event !== "object") return null;
    const record = event as Record<string, unknown>;
    if (record.event === "job.status" && record.data && typeof record.data === "object") {
      const data = record.data as Record<string, unknown>;
      if (data.jobId === jobId && isJobStatus(data.status)) {
        // The server emits "job.status" with status "completed" right before the
        // "turn.committed" event that carries the actual text. If we surface the
        // bare completed status first, applySnapshot would treat it as a terminal
        // error and abort the stream before the turn arrives. Skip it here; the
        // completed turn arrives via "turn.committed" (or the polling fallback).
        if (data.status === "completed") return null;
        const snapshot: TurnJobSnapshot = { id: jobId, simulationId, status: data.status };
        if (typeof data.errorCode === "string") snapshot.errorCode = data.errorCode;
        return snapshot;
      }
    }
    if (record.event === "turn.committed" && record.data && typeof record.data === "object") {
      const snapshot = parseTurnJobSnapshot({ id: jobId, simulationId, status: "completed", committedTurn: record.data });
      if (snapshot) return snapshot;
    }
    return null;
  }

  async function startGeneration(action: string, interaction: TurnInteraction) {
    requestRef.current?.abort();
    const controller = new AbortController();
    requestRef.current = controller;
    submittingRef.current = true;
    setLastRealCost(null);
    balanceBeforeRef.current = null;
    setStatus("queued");
    setErrorMessage(null);
    setPendingAction(action);
    pendingActionRef.current = action;
    setFailedJobId(null);
    try {
      try {
        const me = await api.getMe();
        balanceBeforeRef.current = me.availableBalance;
      } catch {
        balanceBeforeRef.current = null;
      }
      const snapshot = parseTurnJobSnapshot(await api.createTurnJob(
        journey.simulationId,
        { action, interaction },
        { idempotencyKey: createIdempotencyKey(), signal: controller.signal },
      ));
      if (!snapshot) throw new Error("Invalid turn job response");
      setActiveJobId(snapshot.id);
      safelySaveJob(snapshot.id, action);
      if (!applySnapshot(snapshot)) await followJob(snapshot.id, controller);
    } catch (error) {
      if (controller.signal.aborted) return;
      submittingRef.current = false;
      setStatus("failed");
      setActiveJobId(null);
      setErrorMessage(userFacingError(error));
    }
  }

  function submitAction(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const action = draft.trim();
    if (!action || isWorking || submittingRef.current) return;
    submittingRef.current = true;
    void startGeneration(action, "act");
  }

  function chooseBranch(branch: string) {
    if (isWorking || submittingRef.current) return;
    setSelectedBranch(branch);
    setForksOpen(false);
    setDraft("");
    submittingRef.current = true;
    void startGeneration(branch, "branch");
  }

  function continueJourney() {
    if (isWorking || submittingRef.current) return;
    setSelectedBranch("");
    setDraft("");
    submittingRef.current = true;
    void startGeneration("", "continue");
  }

  async function stopGeneration() {
    if (!activeJobId) return;
    const jobId = activeJobId;
    requestRef.current?.abort();
    setStatus("saving");
    try {
      await api.cancelJob(jobId);
      const controller = new AbortController();
      requestRef.current = controller;
      await followJob(jobId, controller);
    } catch (error) {
      setStatus("failed");
      setFailedJobId(jobId);
      setErrorMessage(userFacingError(error));
    }
  }

  async function retryGeneration() {
    const action = draft.trim() || pendingAction || pendingActionRef.current;
    if (!failedJobId) {
      if (action) await startGeneration(action, "act");
      else setErrorMessage("上一幕任务没有恢复，请刷新页面后重试。");
      return;
    }
    requestRef.current?.abort();
    const controller = new AbortController();
    requestRef.current = controller;
    setStatus("queued");
    setErrorMessage(null);
    try {
      const snapshot = parseTurnJobSnapshot(await api.retryJob(failedJobId, { signal: controller.signal }));
      if (!snapshot) throw new Error("Invalid retry response");
      setActiveJobId(snapshot.id);
      safelySaveJob(snapshot.id, action);
      if (!applySnapshot(snapshot)) await followJob(snapshot.id, controller);
    } catch (error) {
      if (controller.signal.aborted) return;
      if (error instanceof ApiError && error.status === 404 && action) {
        setFailedJobId(null);
        safelyClearJob();
        await startGeneration(action, "act");
        return;
      }
      setStatus("failed");
      setErrorMessage(userFacingError(error));
    }
  }

  const availableBranches = latestTurn?.proposal.choices.length ? latestTurn.proposal.choices : currentJourney.branches;
  const openingParagraphs = currentJourney.scene;
  const sceneOneTurns = scenes.find((scene) => scene.no === 1)?.turns ?? [];
  const canSubmitFree = draft.trim().length > 0 && !isWorking;

  function renderSidebar() {
    return (
      <div className="sidebar-stack">
        <details className="sidebar-block" open>
          <summary>世界状态<span className="sidebar-block-chevron" aria-hidden="true"><ChevronDown size={18} /></span></summary>
          <div className="sidebar-block-body">
            <StatePanel journey={currentJourney} idPrefix="desktop" />
          </div>
        </details>
        <details className="sidebar-block" open>
          <summary>幕次<span className="sidebar-block-chevron" aria-hidden="true"><ChevronDown size={18} /></span></summary>
          <div className="sidebar-block-body">
            <SceneToc scenes={scenes} />
          </div>
        </details>
      </div>
    );
  }

  return (
    <div className="journey-shell">
      {entering && (
        <div className="journey-portal" onClick={() => setEntering(false)}>
          <LogoStatePanel state="portal" copies={LOGO_STATE_COPY.portal} size={96} intervalMs={2000} theme="reverse" />
          <p className="journey-portal-world">{currentJourney.worldTitle}</p>
        </div>
      )}
      {unlockNotice.length > 0 && <AchievementBar awards={unlockNotice} onClose={() => setUnlockNotice([])} />}
      <SiteHeader />

      <main className="page-container page-container--detail journey-layout">
        <section className="reader-column" aria-labelledby="journey-title">
          <header className="reader-header">
            <div className="reader-topline">
              <Link id="journey-title" className="back-link" href={currentJourney.worldId ? `/worlds/${encodeURIComponent(currentJourney.worldId)}` : "/journey"} aria-label={`返回${currentJourney.worldTitle}的世界详情`}><span aria-hidden="true"><ChevronLeft size={18} /></span>{currentJourney.worldTitle}</Link>
              <button className="journey-state-trigger" type="button" onClick={() => setStateOpen(true)}>状态与幕次</button>
            </div>
          </header>

          <article className="story-reader jj-rise" aria-label="故事正文">
            <section className="journey-scene" id="scene-1" aria-label="第 1 幕 · 旅程开场">
              <SceneHeading scene={{ no: 1, title: "旅程开场", closure: "", turns: [] }} />
              {openingParagraphs.map((paragraph, index) => (
                <p key={paragraph} className="story-para" style={{ animationDelay: `${0.25 + index * 0.55}s` }}>{paragraph}</p>
              ))}
              {currentJourney.quote && <blockquote className="story-para" style={{ animationDelay: `${0.25 + openingParagraphs.length * 0.55}s` }}>{currentJourney.quote}</blockquote>}
              {sceneOneTurns.map((turn) => (
                <TurnBlock key={turn.id} turn={turn} />
              ))}
            </section>

            {scenes.slice(1).map((scene) => (
              <section className="journey-scene" id={`scene-${scene.no}`} key={scene.no} aria-label={`第 ${scene.no} 幕 · ${scene.title}`}>
                <SceneHeading scene={scene} />
                {scene.turns.map((turn) => (
                  <TurnBlock key={turn.id} turn={turn} />
                ))}
              </section>
            ))}

            {isWorking && (
              <div className="story-generation" aria-live="polite">
                <LogoStatePanel
                  state={status === "generating" ? "loading" : "busy"}
                  copies={status === "generating" ? LOGO_STATE_COPY.loading : LOGO_STATE_COPY.busy}
                  size={72}
                  intervalMs={4200}
                />
              </div>
            )}
          </article>

          {latestTurn && (
            <div className="story-ender">
              {latestTurn.id === generatedTurnId && lastRealCost !== null && <p className="turn-cost">本次花费 {lastRealCost} 微光</p>}
              <div className="status-divider" aria-hidden="true"><span>世界状态：{statusLine}</span></div>
            </div>
          )}

          <section className="interaction-panel" aria-label="你的选择">
            {!(isWorking || status === "failed") && (
              <>
                <button className="interact-option primary" type="button" onClick={continueJourney} disabled={isWorking}>
                  <span className="option-icon" aria-hidden="true"><RefreshCw size={18} /></span>
                  <span className="option-title">让世界继续</span>
                  <span className="option-hint">
                    {hasForks
                      ? "不选岔路、不写行动，剧情沿显而易见的路径自己推进"
                      : "剧情沿显而易见的路径推进；世界此刻不可干预"}
                  </span>
                </button>

                {hasForks && (
                  <div className="option-wrap" onMouseEnter={() => setForksOpen(true)} onMouseLeave={() => setForksOpen(false)}>
                    <button
                      type="button"
                      className="interact-option secondary"
                      aria-expanded={forksOpen}
                      onClick={() => setForksOpen((value) => !value)}
                      disabled={isWorking}
                    >
                      <span className="option-title">{selectedBranch || "看看其他选择"}</span>
                      <span className="option-suffix" aria-hidden="true"><ChevronDown size={18} /></span>
                    </button>
                    {forksOpen && (
                      <ul className="fork-options" role="listbox" aria-label="岔路选择">
                        {availableBranches.map((branch, index) => (
                          <li key={branch}>
                            <button type="button" role="option" aria-selected={branch === selectedBranch} onClick={() => chooseBranch(branch)} disabled={isWorking}>
                              <span>{String(index + 1).padStart(2, "0")}</span>
                              <strong>{branch}</strong>
                            </button>
                          </li>
                        ))}
                      </ul>
                    )}
                  </div>
                )}
              </>
            )}

            <form className="option-form" onSubmit={submitAction}>
              <input
                className="action-input"
                value={draft}
                onChange={(event) => setDraft(event.target.value)}
                placeholder={hasForks ? "或者，写下自己的行动，例如：先查看信封背面的邮戳……" : "或者，写下此刻的想法（不会改变这一段的走向）"}
                aria-label={hasForks ? "或者，写下自己的行动" : "或者，写下此刻的想法（不会改变这一段的走向）"}
                disabled={isWorking}
              />
              {isWorking ? (
                <button className="option-submit" type="button" onClick={() => void stopGeneration()} disabled={!activeJobId}>停止生成</button>
              ) : status === "failed" ? (
                <button className="option-submit" type="button" onClick={() => void retryGeneration()}>重试这一幕</button>
              ) : (
                <button className="option-submit" type="submit" disabled={!canSubmitFree}>确认行动</button>
              )}
            </form>
            {errorMessage && <p className="journey-error" role="alert">{errorMessage}</p>}
          </section>

        </section>

        <aside className="state-column" aria-label="旅程侧栏">
          {renderSidebar()}
        </aside>
      </main>

      {stateOpen && (
        <Dialog title="状态与幕次" onClose={() => setStateOpen(false)}>
          <div className="sidebar-stack">
            <details className="sidebar-block" open>
              <summary>世界状态<span className="sidebar-block-chevron" aria-hidden="true"><ChevronDown size={18} /></span></summary>
              <div className="sidebar-block-body">
                <StatePanel journey={currentJourney} idPrefix="mobile" />
              </div>
            </details>
            <details className="sidebar-block" open>
              <summary>幕次<span className="sidebar-block-chevron" aria-hidden="true"><ChevronDown size={18} /></span></summary>
              <div className="sidebar-block-body">
                <SceneToc scenes={scenes} onJump={() => setStateOpen(false)} />
              </div>
            </details>
          </div>
        </Dialog>
      )}

      <MobileNav />
    </div>
  );
}
