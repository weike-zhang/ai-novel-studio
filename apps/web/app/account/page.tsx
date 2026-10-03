"use client";

import { FormEvent, useEffect, useMemo, useState } from "react";
import Link from "next/link";
import Image from "next/image";
import { useRouter } from "next/navigation";
import { ApiError, type Achievement, type Benefits, type Me } from "@ai-novel-studio/api-client";
import { createApiClient } from "../lib/api";
import { SiteHeader } from "../lib/site-header";
import { MobileNav } from "../lib/mobile-nav";
import { Dialog } from "../lib/dialog";
import CheckInDialog from "../checkin/checkin-dialog";
import "./account.css";

function apiError(error: unknown): string {
  if (!(error instanceof ApiError)) return "操作暂时不可用，请稍后再试。";
  if (error.status === 0) return "暂时无法连接服务。";
  if (error.status === 401) return "登录已失效，请重新登录。";
  if (error.code === "authentication_failed") return "当前密码不正确。";
  return error.message || "操作失败，请重试。";
}

type DialogKind = "password" | "checkin" | null;

export default function AccountPage() {
  const router = useRouter();
  const [me, setMe] = useState<Me | null>(null);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [loading, setLoading] = useState(true);
  const [dialog, setDialog] = useState<DialogKind>(null);
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmNewPassword, setConfirmNewPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [benefits, setBenefits] = useState<Benefits | null>(null);
  const [achievements, setAchievements] = useState<Achievement[]>([]);

  const client = useMemo(() => createApiClient(), []);

  useEffect(() => {
    let cancelled = false;
    Promise.all([client.getMe(), client.getBenefits(), client.getMyAchievements()])
      .then(([meResult, benefitsResult, achievementResult]) => {
        if (cancelled) return;
        setMe(meResult);
        setBenefits(benefitsResult);
        setAchievements(achievementResult.achievements);
      })
      .catch((caught: unknown) => {
        if (cancelled) return;
        setError(apiError(caught));
        if (caught instanceof ApiError && caught.status === 401) {
          router.push("/login");
        }
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [client, router]);

  async function changePassword(event: FormEvent) {
    event.preventDefault();
    if (!currentPassword || !newPassword) {
      setError("请填写当前密码和新密码。");
      return;
    }
    if (newPassword.length < 6 || newPassword.length > 72 || !/[A-Za-z]/.test(newPassword) || !/[0-9]/.test(newPassword)) {
      setError("新密码需 6-72 位，且至少包含一个字母和一个数字。");
      return;
    }
    if (newPassword !== confirmNewPassword) {
      setError("两次输入的新密码不一致。");
      return;
    }
    setError("");
    setNotice("");
    setBusy(true);
    try {
      await client.changePassword({ currentPassword, replacementPassword: newPassword });
      setCurrentPassword("");
      setNewPassword("");
      setConfirmNewPassword("");
      setNotice("密码已修改，其他设备已退出登录。");
      setDialog(null);
    } catch (caught) {
      setError(apiError(caught));
    } finally {
      setBusy(false);
    }
  }

  async function logout() {
    setBusy(true);
    try {
      await client.logout();
      router.push("/login");
      router.refresh();
    } catch (caught) {
      setError(apiError(caught));
      setBusy(false);
    }
  }

  if (loading) {
    return (
      <div className="site-shell">
        <SiteHeader />
        <main className="page-container page-container--account"><div className="state-panel">正在加载账户信息…</div></main>
        <MobileNav />
      </div>
    );
  }

  return (
    <div className="site-shell">
      <SiteHeader />
      <main className="page-container page-container--account">
        <header className="page-heading">
          <h1>账户</h1>
        </header>

        {error && <p className="alert error" role="alert">{error}</p>}
        {notice && <p className="alert success" role="status">{notice}</p>}

        {me && (
          <section className="account-identity">
            <div className="account-avatar">{me.nickname || me.accountName.slice(0, 1)}</div>
            <div className="account-identity-body">
              <strong>{me.accountName}</strong>
              <span>{me.email}</span>
            </div>
            <div className="account-identity-balance">
              <span className="glimmer"><i /> {me.availableBalance} 微光</span>
            </div>
            <div className="account-checkin">
              <div>
                <strong>每日签到</strong>
                <span>{benefits ? `${benefits.coverAllowance} 次封面限免 · 每日可得 10-15 微光` : "每日签到可得微光"}</span>
              </div>
              <button className="button primary" type="button" onClick={() => setDialog("checkin")}>
                签到
              </button>
            </div>
          </section>
        )}

        <nav className="account-menu" aria-label="账户操作">
          <button type="button" className="account-menu-item" onClick={() => setDialog("password")}>
            <strong>修改密码</strong>
            <span>修改后其他设备会退出登录</span>
          </button>
          <Link className="account-menu-item" href="/journey">
            <strong>我的旅程</strong>
            <span>继续你的故事</span>
          </Link>
        </nav>

        <section className="account-achievements" aria-labelledby="achievement-title">
          <header className="account-section-heading">
            <div><h2 id="achievement-title">你走过的路，都算数</h2></div>
            <span>{achievements.length} 枚徽记</span>
          </header>
          {achievements.length === 0 ? (
            <p className="achievement-empty">完成第一幕、第一次让世界自行推进、或做出关键选择时，成就徽记会在这里点亮。</p>
          ) : (
            <ul className="achievement-wall">
              {achievements.map((achievement) => (
                <li className="achievement-card" key={`${achievement.scope}-${achievement.worldId ?? ""}-${achievement.code}`}>
                  <span className="achievement-card-mark" aria-hidden="true"><Image src="/brand/novelstudio-mark.svg" alt="" width={20} height={20} /></span>
                  <div className="achievement-card-body">
                    <strong>{achievement.title}</strong>
                    <p>{achievement.description}</p>
                    {achievement.worldTitle && <span className="achievement-card-world">{achievement.worldTitle}</span>}
                  </div>
                  {achievement.reward > 0 && <span className="achievement-card-reward">+{achievement.reward} 微光</span>}
                </li>
              ))}
            </ul>
          )}
        </section>

        <div className="account-actions">
          <button className="button secondary" onClick={() => void logout()} disabled={busy}>退出登录</button>
        </div>
      </main>

      {dialog === "checkin" && (
        <Dialog title="每日签到" onClose={() => { setDialog(null); setError(""); }}>
          <CheckInDialog onBalanceChange={(credits) => setMe((current) => current ? { ...current, availableBalance: credits } : current)} />
        </Dialog>
      )}

      {dialog === "password" && (
        <Dialog title="修改密码" onClose={() => { setDialog(null); setError(""); }}>
          <form className="account-form" onSubmit={changePassword} noValidate>
            <label className="field">
              <span className="field-label">当前密码</span>
              <input type="password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} autoComplete="current-password" />
            </label>
            <label className="field">
              <span className="field-label">新密码</span>
              <input type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} autoComplete="new-password" />
            </label>
            <label className="field">
              <span className="field-label">确认新密码</span>
              <input type="password" value={confirmNewPassword} onChange={(e) => setConfirmNewPassword(e.target.value)} autoComplete="new-password" />
            </label>
            <button className="button primary" type="submit" disabled={busy}>修改密码</button>
          </form>
        </Dialog>
      )}

      <MobileNav />
    </div>
  );
}
