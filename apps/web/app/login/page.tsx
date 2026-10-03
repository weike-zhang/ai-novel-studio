"use client";

import { FormEvent, Suspense, useState } from "react";
import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { ApiError, type LoginResult } from "@ai-novel-studio/api-client";
import { createApiClient } from "../lib/api";
import { LOGO_STATE_COPY, LogoStatePanel } from "../lib/logo-state-mark";
import { MobileNav } from "../lib/mobile-nav";
import { SiteHeader } from "../lib/site-header";
import "../register/register.css";

function userFacingError(error: unknown): string {
  if (!(error instanceof ApiError)) return "登录暂时不可用，请稍后再试。";
  if (error.status === 0) return "暂时无法连接服务，请检查网络后重试。";
  if (error.status === 401) return "账号或密码不正确。";
  return error.message || "登录失败，请重试。";
}

function LoginForm() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const [accountName, setAccountName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [result, setResult] = useState<LoginResult | null>(null);

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!accountName.trim() || !password) {
      setError("请填写账号和密码。");
      return;
    }
    setError("");
    setSubmitting(true);
    try {
      const client = createApiClient();
      const loginResult = await client.login({ accountName: accountName.trim(), password });
      setResult(loginResult);
      const next = searchParams.get("next");
      router.push(next && next.startsWith("/") ? next : "/");
      router.refresh();
    } catch (caught) {
      setError(userFacingError(caught));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="register-card">
      <div className="register-header">
        <Link className="brand-link" href="/">AI 小说创作台</Link>
        <p className="section-kicker">欢迎回来</p>
        <h1>登录</h1>
        <p className="register-subtitle">用账号和密码继续你的旅程。默认保持登录，退出或重置密码时自动失效。</p>
      </div>

      {error && <p className="register-error" role="alert">{error}</p>}
      {result && <p className="register-error">已登录为 {result.accountName}，正在返回…</p>}

      <form className="register-form" onSubmit={submit} noValidate>
        <label className="field">
          <span className="field-label">账号</span>
          <input
            type="text"
            value={accountName}
            onChange={(event) => setAccountName(event.target.value)}
            autoComplete="username"
            maxLength={20}
          />
        </label>
        <label className="field">
          <span className="field-label">密码</span>
          <input
            type="password"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
            autoComplete="current-password"
          />
        </label>
        {submitting ? (
          <span className="submit-working" role="status"><LogoStatePanel state="portal" copies={LOGO_STATE_COPY.portal} size={26} intervalMs={3000} /></span>
        ) : (
          <button className="button primary submit-button" type="submit">登录</button>
        )}
      </form>

      <p className="register-login">
        还没有账号？<Link href="/register">创建账号</Link>
      </p>
    </div>
  );
}

export default function LoginPage() {
  return (
    <div className="site-shell">
      <SiteHeader />
      <main className="page-container page-container--auth">
        <Suspense fallback={<div className="register-card">正在加载…</div>}>
          <LoginForm />
        </Suspense>
      </main>
      <MobileNav />
    </div>
  );
}
