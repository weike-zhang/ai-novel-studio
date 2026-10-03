"use client";

import Link from "next/link";
import Image from "next/image";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import { ApiClient, ApiError, type Achievement, type World } from "@ai-novel-studio/api-client";
import { createApiClient } from "../../lib/api";
import { SiteHeader } from "../../lib/site-header";

import ReportWorld from "./report-world";
import { popularityLabel } from "../../lib/popularity";
import { type LogoState, LOGO_STATE_COPY, LogoStatePanel } from "../../lib/logo-state-mark";

type Character = { name: string; role: string; personality: string; goal: string; creed?: string; relation: string; public: boolean };
type Discovery = { label?: string; hook?: string; promise?: string; playTime?: string; format?: string };
type Opening = { text?: string; actions?: string[] };
type WorldAchievement = { key?: string; title?: string; hint?: string; reward?: number };
type Setting = {
  contentRating?: string;
  contentBoundaries?: string;
  discovery?: Discovery;
  worldBackground?: { eraPlace?: string; rules?: string; conflict?: string };
  playerIdentity?: { name?: string; background?: string; goal?: string; freedom?: string };
  narrative?: { pointOfView?: string; turnLength?: string; openness?: string };
  opening?: Opening;
  characters?: Character[];
  achievements?: WorldAchievement[];
};

function createWorldApi(): ApiClient {
  return createApiClient();
}

function readableDate(value: string): string {
  const date = new Date(value);
  return Number.isNaN(date.valueOf()) ? value : new Intl.DateTimeFormat("zh-CN", { dateStyle: "long" }).format(date);
}

export default function WorldDetail({ worldId }: { worldId: string }) {
  const api = useMemo(() => createWorldApi(), []);
  const [world, setWorld] = useState<World | null>(null);
  const [state, setState] = useState<"loading" | "ready" | "missing" | "error">("loading");
  const [journeyState, setJourneyState] = useState<"idle" | "creating" | "error">("idle");
  const [moderating, setModerating] = useState(false);
  const [moderateError, setModerateError] = useState("");
  const [coverBusy, setCoverBusy] = useState(false);
  const [batchBusy, setBatchBusy] = useState(false);
  const [featuredBusy, setFeaturedBusy] = useState(false);
  const [adminMessage, setAdminMessage] = useState<{ kind: "success" | "error"; text: string } | null>(null);
  const [earnedAchievements, setEarnedAchievements] = useState<Achievement[]>([]);
  const [isAdmin, setIsAdmin] = useState(false);
  const router = useRouter();

  useEffect(() => {
    const controller = new AbortController();
    void api.getWorld(worldId, { signal: controller.signal }).then((value) => {
      setWorld(value);
      setState("ready");
    }).catch((error: unknown) => {
      if (controller.signal.aborted) return;
      setState(error instanceof ApiError && error.status === 404 ? "missing" : "error");
    });
    return () => controller.abort();
  }, [api, worldId]);

  useEffect(() => {
    const controller = new AbortController();
    void api.getMyAchievements({ signal: controller.signal })
      .then((result) => setEarnedAchievements(result.achievements))
      .catch(() => setEarnedAchievements([]));
    void api.getIsAdmin({ signal: controller.signal })
      .then((result) => setIsAdmin(result.isAdmin))
      .catch(() => setIsAdmin(false));
    return () => controller.abort();
  }, [api]);

  const moderateWorld = async (suspend: boolean) => {
    if (!world || moderating) return;
    setModerating(true);
    setModerateError("");
    try {
      const updated = suspend ? await api.suspendWorld(world.id) : await api.restoreWorld(world.id);
      setWorld({ ...world, status: updated.status, visibility: updated.visibility });
    } catch {
      setModerateError(suspend ? "下架失败，请稍后重试。" : "恢复失败，请稍后重试。");
    } finally {
      setModerating(false);
    }
  };

  const regenerateCover = async () => {
    if (!world || coverBusy) return;
    setCoverBusy(true);
    setAdminMessage(null);
    try {
      await api.regenerateWorldCover(world.id);
      setAdminMessage({ kind: "success", text: "封面正在后台生成，稍后刷新即可看到。" });
    } catch {
      setAdminMessage({ kind: "error", text: "封面生成请求失败，请稍后重试。" });
    } finally {
      setCoverBusy(false);
    }
  };

  const regenerateAllCovers = async () => {
    if (batchBusy) return;
    setBatchBusy(true);
    setAdminMessage(null);
    try {
      const { queued } = await api.regenerateAllCovers();
      setAdminMessage({ kind: "success", text: `已为 ${queued} 个没有封面的世界开始生成封面。` });
    } catch {
      setAdminMessage({ kind: "error", text: "批量生成封面请求失败，请稍后重试。" });
    } finally {
      setBatchBusy(false);
    }
  };

  const isFeatured = (world?.featuredRank ?? 0) > 0;

  const toggleFeatured = async () => {
    if (!world || featuredBusy) return;
    setFeaturedBusy(true);
    setAdminMessage(null);
    try {
      const next = isFeatured ? await api.removeWorldFeatured(world.id) : await api.setWorldFeatured(world.id);
      setWorld({ ...world, featuredRank: next.featured ? next.rank : 0 });
      setAdminMessage({ kind: "success", text: next.featured ? "已设为编辑推荐。" : "已取消推荐。" });
    } catch {
      setAdminMessage({ kind: "error", text: "更新推荐失败，请稍后重试。" });
    } finally {
      setFeaturedBusy(false);
    }
  };

  const startJourney = async () => {
    if (!world || journeyState === "creating") return;
    setJourneyState("creating");
    try {
      const journey = await api.createJourney({ worldId: world.id });
      router.push(`/journey/${encodeURIComponent(journey.id)}`);
    } catch {
      setJourneyState("error");
    }
  };

  if (state === "loading") return <WorldState logoState="loading" copies={LOGO_STATE_COPY.loading} title="正在打开这个世界…" />;
  if (state === "missing") return <WorldState logoState="idle" copies={LOGO_STATE_COPY.idle} title="这扇门暂时无法进入" detail="世界可能尚未公开，或已经被作者撤回。" />;
  if (state === "error" || !world) return <WorldState logoState="failure" copies={LOGO_STATE_COPY.failure} title="世界详情暂时无法打开" detail="请返回世界广场后重试。" />;

  const setting = world.setting as Setting;
  const discovery = setting.discovery;
  const background = setting.worldBackground;
  const player = setting.playerIdentity;
  const characters = (setting.characters ?? []).filter((character) => character.public !== false);
  const worldAchievements = (setting.achievements ?? []).filter((item): item is WorldAchievement & { key: string } => Boolean(item.key?.trim()));
  const earnedKeys = new Set(
    earnedAchievements
      .filter((item) => item.worldId === world.id && item.scope !== "emergent")
      .map((item) => item.code),
  );
  const earnedEmergents = earnedAchievements.filter((item) => item.worldId === world.id && item.scope === "emergent");

  return (
    <div className="site-shell world-detail-shell">
      <SiteHeader />
      <main className="page-container page-container--detail world-detail-main">
        <div className="world-back-row"><Link href="/"><span aria-hidden="true">←</span> 返回世界广场</Link></div>

        <section className="world-hero jj-rise" aria-labelledby="world-title">
          <div className="world-hero-cover">
            {world.cover
              ? <Image src={world.cover} alt={`${world.title} 封面`} fill sizes="(max-width: 860px) 100vw, 420px" style={{ objectFit: "cover" }} />
              : <div className="world-cover rust"><div className="cover-gate"><i /><b /><em /></div></div>}
          </div>
          <div className="world-summary">
            <h1 id="world-title">{world.title}</h1>
            {discovery?.hook && <p className="world-hook">{discovery.hook}</p>}
            <p className="world-meta-line">
              <span>by {world.authorNickname}</span>
              <span className={`popularity popularity-${popularityLabel(world.simulationCount).level}`}>{popularityLabel(world.simulationCount).label}</span>
              <span>更新于 {readableDate(world.updatedAt)}</span>
              {setting.contentRating && <span className="world-age-rating" aria-label="内容分级">{setting.contentRating}</span>}
            </p>
            <div className="world-hero-actions">
              {journeyState === "creating" ? (
                <LogoStatePanel state="portal" copies={LOGO_STATE_COPY.portal} size={44} intervalMs={3000} />
              ) : (
                <button className="world-primary-button" type="button" onClick={startJourney}>开始旅程<span aria-hidden="true">→</span></button>
              )}
              {journeyState === "error" && <p className="world-error">创建失败，请稍后重试。</p>}
            </div>
          </div>
        </section>

        <article className="world-reading jj-rise" style={{ animationDelay: "0.08s" }}>
          <section>
            <h2>世界背景</h2>
            <div className="world-facts">
              <div><span>时间与地点</span><p>{background?.eraPlace || "未知地点"}</p></div>
              <div><span>这个世界的规则</span><p>{background?.rules || "进入后逐步发现"}</p></div>
              <div><span>眼前的冲突</span><p>{background?.conflict || world.summary}</p></div>
            </div>
          </section>
          <section>
            <h2>你的开局身份</h2>
            {player ? (
              <div className="player-facts">
                <strong>{player.name || "你"}</strong>
                <p>{player.background}</p>
                {player.goal && <div><span>当前目标</span><p>{player.goal}</p></div>}
                {player.freedom && <div><span>你可以</span><p>{player.freedom}</p></div>}
              </div>
            ) : <p>你将在进入后决定自己与这个世界的关系。</p>}
          </section>
          <section>
            <h2>公开角色</h2>
            {characters.length > 0 ? (
              <ul className="character-list">
                {characters.map((character) => (
                  <li key={character.name}>
                    <strong>{character.name}</strong>
                    <span>{character.role}</span>
                    <p>{[character.personality, character.goal ? `目标：${character.goal}` : "", character.creed ? `信条：${character.creed}` : "", character.relation].filter(Boolean).join("。")}</p>
                  </li>
                ))}
              </ul>
            ) : <p>作者暂未公开角色档案。</p>}
          </section>
          {(worldAchievements.length > 0 || earnedEmergents.length > 0) && (
            <section className="world-achievements" aria-labelledby="world-achievement-title">
              <div className="world-section-heading"><div><p className="section-kicker">世界成就</p><h2 id="world-achievement-title">在这个世界里留下印记</h2></div><span>达成会收到微光</span></div>
              {worldAchievements.length > 0 && (
                <ul className="world-achievement-list">
                  {worldAchievements.map((achievement) => {
                    const earned = earnedKeys.has(achievement.key);
                    return (
                      <li key={achievement.key} className={earned ? "world-achievement earned" : "world-achievement"}>
                          <span className="world-achievement-mark" aria-hidden="true"><Image src="/brand/novelstudio-mark.svg" alt="" width={20} height={20} /></span>
                        <div className="world-achievement-body">
                          <strong>{achievement.title || achievement.key}</strong>
                          {achievement.hint && <p>{achievement.hint}</p>}
                        </div>
                        <span className="world-achievement-reward">{(achievement.reward ?? 0) > 0 ? `+${achievement.reward} 微光` : "已获得"}</span>
                      </li>
                    );
                  })}
                </ul>
              )}
              {earnedEmergents.length > 0 && (
                <ul className="world-achievement-list">
                  {earnedEmergents.map((achievement) => (
                    <li key={`${achievement.worldId}-${achievement.code}`} className="world-achievement earned">
                      <span className="world-achievement-mark" aria-hidden="true"><Image src="/brand/novelstudio-mark.svg" alt="" width={20} height={20} /></span>
                      <div className="world-achievement-body">
                        <strong>{achievement.title}</strong>
                        {achievement.description && <p>{achievement.description}</p>}
                      </div>
                      <span className="world-achievement-reward">已获得</span>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          )}
        </article>

        <div className="world-report-row"><ReportWorld worldId={world.id} worldTitle={world.title} /></div>
        {isAdmin && (
          <div className="world-moderate-row">
            <button className="button secondary" type="button" disabled={moderating} onClick={() => void moderateWorld(world.status === "published")}>
              {world.status === "published" ? "下架世界" : "恢复上架"}
            </button>
            <button className="button secondary" type="button" disabled={coverBusy} onClick={() => void regenerateCover()}>
              {coverBusy ? "正在请求…" : "生成并替换封面"}
            </button>
            <button className="button secondary" type="button" disabled={batchBusy} onClick={() => void regenerateAllCovers()}>
              {batchBusy ? "正在请求…" : "为全部无封面世界生成封面"}
            </button>
            <button className="button secondary" type="button" disabled={featuredBusy} onClick={() => void toggleFeatured()}>
              {featuredBusy ? "处理中…" : isFeatured ? "取消推荐" : "设为推荐"}
            </button>
            {moderateError && <p className="world-error">{moderateError}</p>}
            {adminMessage && <p className={`world-error ${adminMessage.kind === "success" ? "world-cover-success" : ""}`}>{adminMessage.text}</p>}
          </div>
        )}
      </main>
      <div className="world-mobile-action"><div><strong>{world.title}</strong><span>{journeyState === "error" ? "创建失败，请重试" : "第一幕预览无需登录"}</span></div><button type="button" onClick={startJourney} disabled={journeyState === "creating"}>{journeyState === "creating" ? "正在创建…" : "开始旅程"}</button></div>
    </div>
  );
}

function WorldState({ logoState, copies, title, detail }: { logoState: LogoState; copies: string[]; title: string; detail?: string }) {
  return <div className="world-detail-state" role="status"><LogoStatePanel state={logoState} copies={copies} size={56} intervalMs={4000} /><strong>{title}</strong>{detail && <p>{detail}</p>}<Link href="/">返回世界广场</Link></div>;
}
