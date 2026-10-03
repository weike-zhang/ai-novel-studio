"use client";

import { useEffect, useMemo, useState } from "react";
import { ApiError, type CheckInList } from "@ai-novel-studio/api-client";
import { createApiClient } from "../lib/api";
import { LogoStateMark } from "../lib/logo-state-mark";
import "./checkin.css";

type Range = 7 | 30;

function rewardForStreak(streakDays: number): { reward: number; bonus: number } {
  const reward = streakDays % 2 === 0 ? 15 : 10;
  let bonus = 0;
  if (streakDays > 0 && streakDays % 7 === 0) bonus += 30;
  return { reward, bonus };
}

function formatDate(date: Date): string {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}`;
}

function addDays(base: Date, delta: number): Date {
  const copy = new Date(base);
  copy.setDate(copy.getDate() + delta);
  return copy;
}

// 7 日：昨天 · 今天 · 未来 5 天
function sevenDayCells(): Date[] {
  const today = new Date();
  return Array.from({ length: 7 }, (_, index) => addDays(today, index - 1));
}

export default function CheckInDialog({ onBalanceChange }: { onBalanceChange?: (credits: number) => void }) {
  const api = useMemo(() => createApiClient(), []);
  const [history, setHistory] = useState<CheckInList | null>(null);
  const [range, setRange] = useState<Range>(7);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [error, setError] = useState("");

  useEffect(() => {
    let cancelled = false;
    api.getCheckins()
      .then((checkinsResult) => {
        if (cancelled) return;
        setHistory(checkinsResult);
      })
      .catch((caught: unknown) => {
        if (cancelled) return;
        setError(caught instanceof ApiError && caught.status === 401 ? "请先登录后签到。" : "签到数据暂时无法加载。");
      });
    return () => {
      cancelled = true;
    };
  }, [api]);

  const todayDate = formatDate(new Date());
  const records = history?.checkins ?? [];
  const signedDates = new Set(records.map((record) => record.date));
  const currentStreak = records[0]?.streakDays ?? 0;

  // 累计签到所得微光
  const totalEarned = records.reduce((sum, record) => sum + record.rewardCredits + record.bonusCredits, 0);

  async function doCheckIn() {
    if (busy) return;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      const result = await api.checkIn();
      const refreshed = await api.getCheckins();
      setHistory(refreshed);
      const refreshedBenefits = await api.getBenefits();
      onBalanceChange?.(refreshedBenefits.credits);
      setNotice(result.alreadyToday
        ? "今日已来过，明天再来。"
        : `这次停留，为你留住 ${result.rewardCredits + result.bonusCredits} 微光。`);
    } catch (caught) {
      setError(caught instanceof ApiError && caught.status === 401 ? "登录状态已失效，请重新登录。" : "签到失败，请稍后重试。");
    } finally {
      setBusy(false);
    }
  }

  const cells = range === 7 ? sevenDayCells() : Array.from({ length: 30 }, (_, index) => addDays(new Date(), index - 29));

  return (
    <div className="checkin-dialog">
      {error && <p className="alert error" role="alert">{error}</p>}
      {notice && <p className="alert success" role="status">{notice}</p>}

      <div className="checkin-logo"><LogoStateMark state="idle" size={40} /></div>

      <p className="checkin-summary">
        <span>连续停留 <b>{currentStreak}</b> 天</span>
        <i aria-hidden="true" />
        <span>累计留住 <b>{totalEarned}</b> 微光</span>
      </p>

      <div className="range-tabs" role="tablist" aria-label="签到周期">
        <button type="button" role="tab" aria-selected={range === 7} className={range === 7 ? "active" : ""} onClick={() => setRange(7)}>近 7 日</button>
        <button type="button" role="tab" aria-selected={range === 30} className={range === 30 ? "active" : ""} onClick={() => setRange(30)}>近 30 日</button>
      </div>

      <ol className={`checkin-cells checkin-cells-${range}`}>
        {cells.map((date) => {
          const key = formatDate(date);
          const signed = signedDates.has(key);
          const isToday = key === todayDate;
          const isFuture = key > todayDate;
          const record = records.find((item) => item.date === key);
          const reward = record?.rewardCredits ?? (isToday ? rewardForStreak(currentStreak + 1).reward : null);
          const bonus = record?.bonusCredits ?? 0;
          return (
            <li
              key={key}
              className={`checkin-cell ${signed ? "signed" : ""} ${isToday ? "today" : ""} ${!signed && isFuture ? "future" : ""}`}
              aria-current={isToday ? "date" : undefined}
            >
              <span className="cell-date">{isToday ? "今天" : `${date.getMonth() + 1}/${date.getDate()}`}</span>
              {signed ? (
                <span className="cell-mark" aria-label="已签到">✓</span>
              ) : isToday ? (
                <button className="cell-checkin" type="button" disabled={busy} onClick={() => void doCheckIn()}>
                  {busy ? "签到中…" : "签到"}
                </button>
              ) : (
                <span className="cell-reward">{reward === null ? "+10–15" : `+${reward}`}</span>
              )}
              {bonus > 0 && <span className="cell-bonus">+{bonus}</span>}
            </li>
          );
        })}
      </ol>

      <p className="checkin-legend">
        <span><i className="dot dot-signed" />已签到</span>
        <span><i className="dot dot-today" />今天</span>
        <span className="checkin-rules">每日 10–15 微光 · 连签 7 天 +30 · 单月满 20 天 +60</span>
      </p>
    </div>
  );
}
