"use client";

import Image from "next/image";
import Link from "next/link";
import { useEffect, useMemo, useState } from "react";
import { ApiError, type SimulationSummary, type World } from "@ai-novel-studio/api-client";
import { createApiClient } from "../lib/api";
import { SiteHeader } from "../lib/site-header";
import { MobileNav } from "../lib/mobile-nav";
import "./journey-list.css";

export default function JourneyListPage() {
  const api = useMemo(() => createApiClient(), []);
  const [simulations, setSimulations] = useState<SimulationSummary[]>([]);
  const [covers, setCovers] = useState<Record<string, string>>({});
  const [state, setState] = useState<"loading" | "ready" | "unauthenticated" | "error">("loading");
  const [retryKey, setRetryKey] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    api.listMySimulations({ signal: controller.signal })
      .then(async (result) => {
        setSimulations(result.simulations);
        const worldIds = Array.from(new Set(result.simulations.map((item) => item.worldId).filter(Boolean)));
        if (worldIds.length > 0) {
          try {
            const worlds = await api.listWorlds({ limit: 100 }, { signal: controller.signal });
            setCovers(Object.fromEntries(worlds.worlds.filter((item): item is World & { cover: string } => Boolean(item.cover)).map((item) => [item.id, item.cover])));
          } catch {
            setCovers({});
          }
        }
        setState("ready");
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) return;
        setState(error instanceof ApiError && error.status === 401 ? "unauthenticated" : "error");
      });
    return () => controller.abort();
  }, [api, retryKey]);

  return (
    <div className="site-shell">
      <SiteHeader />
      <main className="page-container page-container--list">
        <header className="page-heading">
          <h1>继续你的故事</h1>
        </header>

        {state === "loading" && <div className="state-panel" role="status">正在加载你的旅程…</div>}
        {state === "unauthenticated" && (
          <div className="state-panel" role="status">
            <strong>登录后查看你的旅程</strong>
            <span>每一段旅程都属于登录账号。</span>
            <Link className="button primary" href="/login">去登录</Link>
          </div>
        )}
        {state === "error" && (
          <div className="state-panel" role="alert">
            <strong>旅程暂时无法加载</strong>
            <span>你的旅程仍保存在服务器，可以稍后重试。</span>
            <button className="button secondary" type="button" onClick={() => { setState("loading"); setRetryKey((value) => value + 1); }}>重新加载</button>
          </div>
        )}
        {state === "ready" && simulations.length === 0 && (
          <div className="state-panel" role="status">
            <strong>还没有旅程</strong>
            <span>从世界广场选择一个世界，开启第一段属于你的故事。</span>
            <Link className="button primary" href="/">探索世界</Link>
          </div>
        )}
        {state === "ready" && simulations.length > 0 && (
          <ul className="journey-list">
            {simulations.map((simulation) => (
              <li key={simulation.id}>
                <Link className="journey-list-card" href={`/journey/${encodeURIComponent(simulation.id)}`}>
                  <div className="journey-list-mark">
                    {covers[simulation.worldId]
                      ? <Image src={covers[simulation.worldId]} alt="" width={64} height={64} />
                      : <Image src="/brand/novelstudio-mark.svg" alt="" width={44} height={44} />}
                  </div>
                  <div className="journey-list-body">
                    <strong>{simulation.worldTitle || "未命名世界"}</strong>
                    <p>{simulation.summary || "旅程进行中"}</p>
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </main>
      <MobileNav />
    </div>
  );
}
