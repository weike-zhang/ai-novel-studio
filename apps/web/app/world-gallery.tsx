"use client";

import Image from "next/image";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import type { WorldCard } from "@ai-novel-studio/api-client";
import { createApiClient } from "./lib/api";
import { useSession } from "./lib/use-session";
import { LOGO_STATE_COPY, LogoStatePanel } from "./lib/logo-state-mark";

type CategoryDef = { label: string; type?: string; tags?: string[]; sort?: "popular" | "newest" };

const categories: CategoryDef[] = [
  { label: "推荐", sort: "popular" },
  { label: "全部", sort: "popular" },
  { label: "古言", type: "古言", sort: "popular" },
  { label: "玄幻", type: "玄幻", sort: "popular" },
  { label: "恋爱", tags: ["恋爱"], sort: "popular" },
  { label: "科幻", type: "科幻", sort: "popular" },
  { label: "悬疑", type: "悬疑", sort: "popular" },
  { label: "都市", type: "都市", sort: "popular" },
  { label: "末日", type: "末日", sort: "popular" },
  { label: "情感", type: "情感", sort: "popular" },
  { label: "幻想", type: "幻想", sort: "popular" },
];

type Category = string;

const coverTones = ["rust", "ink", "moss", "ash", "amber"] as const;

function decorateWorld(world: WorldCard): WorldCard {
  return world;
}

function WorldCover({ world, index }: { world: WorldCard; index: number }) {
  const cover = world.cover;
  return (
    <div className={`world-cover ${!cover ? coverTones[index % coverTones.length] : ""}`}>
      {cover
        ? <Image src={cover} alt={`${world.title} 封面`} fill sizes="(max-width: 860px) 100vw, 320px" className="cover-image" unoptimized />
        : <span className="cover-gate" aria-hidden="true"><i /><b /><em /></span>}
    </div>
  );
}

function StoryCardBody({ world }: { world: WorldCard }) {
  return (
    <div className="world-body">
      <h3 title={world.title}>{world.title}</h3>
      <p className="world-card-hook" title={world.hook || world.summary}>{world.hook || world.summary}</p>
    </div>
  );
}

function FeaturedBanner({
  worlds,
}: {
  worlds: WorldCard[];
}) {
  const [activeIndex, setActiveIndex] = useState(0);
  const [paused, setPaused] = useState(false);

  useEffect(() => {
    if (paused || worlds.length < 2) return;
    const timer = window.setInterval(() => {
      setActiveIndex((current) => (current + 1) % worlds.length);
    }, 5500);
    return () => window.clearInterval(timer);
  }, [paused, worlds.length]);

  if (worlds.length === 0) return null;

  const world = worlds[activeIndex % worlds.length];
  const cover = world.cover;
  const coverTone = coverTones[activeIndex % coverTones.length];

  return (
    <section
      className={`featured-banner featured-banner-${coverTone}`}
      aria-label="精选世界"
      onMouseEnter={() => setPaused(true)}
      onMouseLeave={() => setPaused(false)}
      onFocus={() => setPaused(true)}
      onBlur={() => setPaused(false)}
    >
      {cover ? (
        <Image
          className="featured-banner-image"
          src={cover}
          alt=""
          fill
          priority
          sizes="(max-width: 860px) 100vw, 1200px"
          unoptimized
        />
      ) : (
        <span className="featured-banner-gate" aria-hidden="true"><i /><b /><em /></span>
      )}
      <div className="featured-banner-overlay" />
      <Link className="featured-banner-content" href={`/worlds/${encodeURIComponent(world.id)}`} aria-label={`查看《${world.title}》的世界详情`}>
        <span className="featured-banner-badge">✦ 精选世界</span>
        <h2>{world.title}</h2>
        <p className="featured-banner-intro">
          <span>{world.authorNickname}</span>
          <span aria-hidden="true"> ｜ </span>
          {world.hook || world.summary}
        </p>
      </Link>
      {worlds.length > 1 && (
        <div className="featured-banner-dots" aria-label="精选世界轮播切换">
          {worlds.map((item, index) => (
            <button
              key={item.id}
              className={index === activeIndex ? "active" : ""}
              type="button"
              aria-label={`查看第 ${index + 1} 个精选世界`}
              aria-current={index === activeIndex ? "true" : undefined}
              onClick={() => setActiveIndex(index)}
            />
          ))}
        </div>
      )}
    </section>
  );
}

function StoryCard({
  world,
  index,
  starting,
  onStart,
}: {
  world: WorldCard;
  index: number;
  starting: boolean;
  onStart: (world: WorldCard) => void;
}) {
  return (
    <article className="world-card">
      <button type="button" className="world-card-link" onClick={() => onStart(world)} disabled={starting}>
        <WorldCover world={world} index={index} />
        <StoryCardBody world={world} />
      </button>
      {starting && (
        <div className="world-card-starting" role="status">
          <LogoStatePanel state="portal" copies={LOGO_STATE_COPY.portal} size={36} intervalMs={2000} theme="reverse" />
        </div>
      )}
    </article>
  );
}

export default function WorldGallery() {
  const api = useMemo(() => createApiClient(), []);
  const router = useRouter();
  const session = useSession();
  const [activeCategory, setActiveCategory] = useState<Category>("推荐");
  const [keyword, setKeyword] = useState("");
  const [worlds, setWorlds] = useState<WorldCard[]>([]);
  const [state, setState] = useState<"loading" | "ready" | "error">("loading");
  const [retryKey, setRetryKey] = useState(0);
  const [startingId, setStartingId] = useState<string | null>(null);

  const startJourney = async (world: WorldCard) => {
    if (startingId) return;
    if (session.status === "anonymous") {
      router.push(`/login?next=${encodeURIComponent("/")}`);
      return;
    }
    if (session.status === "loading") return;
    setStartingId(world.id);
    try {
      const journey = await api.createJourney({ worldId: world.id });
      router.push(`/journey/${encodeURIComponent(journey.id)}`);
    } catch {
      setStartingId(null);
    }
  };

  useEffect(() => {
    const controller = new AbortController();
    const active = categories.find((category) => category.label === activeCategory) ?? categories[0];
    void api.listWorlds({
      sort: active.sort ?? (activeCategory === "推荐" ? "popular" : "newest"),
      limit: 48,
      ...(active.type ? { type: active.type } : {}),
      ...(active.tags ? { tags: active.tags } : {}),
      ...(keyword.trim() ? { keyword: keyword.trim() } : {}),
    }, { signal: controller.signal }).then((page) => {
      setWorlds(page.worlds);
      setState("ready");
    }).catch(() => {
      if (controller.signal.aborted) return;
      setState("error");
    });
    return () => controller.abort();
  }, [activeCategory, keyword, api, retryKey]);

  const decoratedWorlds = worlds.map(decorateWorld);
  const showBanner = activeCategory === "推荐" && !keyword.trim() && decoratedWorlds.length > 0;
  const featuredWorlds = decoratedWorlds
    .filter((world) => (world.featuredRank ?? 0) > 0)
    .sort((a, b) => (a.featuredRank ?? 0) - (b.featuredRank ?? 0))
    .slice(0, 6);
  const bannerWorlds = featuredWorlds.length > 0 ? featuredWorlds : decoratedWorlds.slice(0, 5);
  const bannerKey = bannerWorlds.map((world) => world.id).join("|");
  const catalogWorlds = decoratedWorlds;

  return (
    <>
      {state === "ready" && showBanner && (
        <FeaturedBanner key={bannerKey} worlds={bannerWorlds} />
      )}
      <div className="explore-toolbar">
        <form
          className="explore-search"
          role="search"
          onSubmit={(event) => {
            event.preventDefault();
            setState("loading");
            setRetryKey((value) => value + 1);
          }}
        >
          <span className="explore-search-icon" aria-hidden="true">
            <svg viewBox="0 0 24 24" focusable="false"><circle cx="11" cy="11" r="6.5" /><path d="m16 16 4 4" /></svg>
          </span>
          <input
            type="search"
            value={keyword}
            onChange={(event) => {
              setKeyword(event.target.value);
              setState("loading");
            }}
            placeholder="搜索世界、标签或作者"
            aria-label="搜索世界"
          />
        </form>
        <Link className="button primary explore-create" href="/studio">创建新世界</Link>
      </div>

      <div className="filter-row" aria-label="世界分类">
        {categories.map((category) => (
          <button
            className={`filter ${activeCategory === category.label ? "active" : ""}`}
            type="button"
            key={category.label}
            aria-pressed={activeCategory === category.label}
            onClick={() => {
              setState("loading");
              setActiveCategory(category.label);
            }}
          >{category.label}</button>
        ))}
      </div>

      {state === "loading" && (
        <div className="state-panel" role="status">
          <LogoStatePanel state="loading" copies={LOGO_STATE_COPY.loading} size={48} intervalMs={4000} />
        </div>
      )}
      {state === "error" && (
        <div className="state-panel" role="alert">
          <LogoStatePanel state="failure" copies={LOGO_STATE_COPY.failure} size={48} intervalMs={4000} />
          <strong>世界广场暂时无法打开</strong>
          <span>已发布世界没有丢失，可以稍后重试。</span>
          <button type="button" onClick={() => { setState("loading"); setRetryKey((value) => value + 1); }}>重新加载</button>
        </div>
      )}
      {state === "ready" && decoratedWorlds.length === 0 && (
        <div className="state-panel" role="status">
          <strong>{keyword ? "没有找到匹配的世界" : "世界广场还没有公开世界"}</strong>
          <span>{keyword ? "换个关键词试试。" : "换一个分类看看，或者创建第一扇门。"}</span>
          <Link href="/studio">创建世界</Link>
        </div>
      )}
      {state === "ready" && decoratedWorlds.length > 0 && (
        <section className="catalog-section" aria-labelledby="catalog-title">
          <div className="catalog-heading"><h2 id="catalog-title">挑一个你想活下去的世界</h2></div>
          <div className="world-grid jj-rise" key={activeCategory + keyword}>{catalogWorlds.map((world, index) => <StoryCard key={world.id} world={world} index={index} starting={startingId === world.id} onStart={startJourney} />)}</div>
        </section>
      )}
    </>
  );
}
