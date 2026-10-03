"use client";

import { FormEvent, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { ApiError, type RegistrationResult } from "@ai-novel-studio/api-client";
import { createApiClient } from "../lib/api";
import { LOGO_STATE_COPY, LogoStatePanel } from "../lib/logo-state-mark";
import { MobileNav } from "../lib/mobile-nav";
import { SiteHeader } from "../lib/site-header";
import "./register.css";

type RegistrationStep = "form" | "verifying" | "success";

type FieldError = Partial<Record<"accountName" | "password" | "email" | "code" | "adult", string>>;

function userFacingError(error: unknown): string {
  if (!(error instanceof ApiError)) return "注册暂时不可用，请稍后再试。";
  if (error.status === 0) return "暂时无法连接服务，请检查网络后重试。";
  if (error.status === 403) return "本次注册被风险控制拒绝。若为误判，请联系我们申诉。";
  if (error.status === 409) return "账号名或邮箱已被使用，请更换后重试。";
  if (error.status === 429) return "操作过于频繁，请稍后再试。";
  if (error.code === "adult_confirmation_required") return "请先确认已满 18 周岁并同意条款。";
  if (error.code === "turnstile_failed") return "人机验证未通过，请重试。";
  if (error.code === "verification_cooldown") return "验证码发送过于频繁，请稍后再试。";
  if (error.code === "verification_rate_limited") return "该邮箱发送次数过多，请明天再试。";
  return error.message || "注册失败，请检查填写内容。";
}

const accountNamePattern = /^[A-Za-z0-9_]{4,20}$/;

function turnstileToken(): string {
  return process.env.NEXT_PUBLIC_TURNSTILE_DEV_TOKEN ?? "dev-token";
}

export default function RegisterPage() {
  const [step, setStep] = useState<RegistrationStep>("form");
  const [accountName, setAccountName] = useState("");
  const [password, setPassword] = useState("");
  const [email, setEmail] = useState("");
  const [code, setCode] = useState("");
  const [adultConfirmed, setAdultConfirmed] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<FieldError>({});
  const [nonce, setNonce] = useState("");
  const [countdown, setCountdown] = useState(0);
  const [echoedCode, setEchoedCode] = useState("");
  const [error, setError] = useState("");
  const [sending, setSending] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [result, setResult] = useState<RegistrationResult | null>(null);
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  useEffect(() => {
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, []);

  useEffect(() => {
    if (countdown <= 0) return;
    timerRef.current = setInterval(() => {
      setCountdown((current) => {
        if (current <= 1) {
          if (timerRef.current) clearInterval(timerRef.current);
          return 0;
        }
        return current - 1;
      });
    }, 1000);
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [countdown]);

  function validateForm(): boolean {
    const errors: FieldError = {};
    if (!accountNamePattern.test(accountName)) {
      errors.accountName = "账号为 4-20 位字母、数字或下划线";
    }
    if (password.length < 6 || password.length > 72 || !/[A-Za-z]/.test(password) || !/[0-9]/.test(password)) {
      errors.password = "密码需 6-72 位，且至少包含一个字母和一个数字";
    }
    if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email) || email.length > 320) {
      errors.email = "请输入有效的邮箱地址";
    }
    if (!adultConfirmed) {
      errors.adult = "需确认已满 18 周岁并同意条款后才能注册";
    }
    setFieldErrors(errors);
    return Object.keys(errors).length === 0;
  }

  async function sendCode() {
    if (!validateForm()) return;
    setError("");
    setSending(true);
    try {
      const client = createApiClient();
      const response = await client.sendEmailVerificationCode({
        email: email.trim(),
        turnstileToken: turnstileToken(),
      });
      setNonce(response.nonce);
      setCountdown(response.cooldownSeconds > 0 ? response.cooldownSeconds : 60);
      if (response.verificationCode) {
        setEchoedCode(response.verificationCode);
        setCode(response.verificationCode);
      }
    } catch (caught) {
      setError(userFacingError(caught));
    } finally {
      setSending(false);
    }
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!validateForm()) return;
    if (!nonce) {
      setError("请先获取邮箱验证码");
      return;
    }
    setError("");
    setSubmitting(true);
    try {
      const client = createApiClient();
      const response = await client.register({
        accountName,
        password,
        email: email.trim(),
        code,
        nonce,
        turnstileToken: turnstileToken(),
        adultConfirmed,
        termsVersion: "v1",
      });
      setResult(response);
      setStep("success");
    } catch (caught) {
      setError(userFacingError(caught));
    } finally {
      setSubmitting(false);
    }
  }

  if (step === "success" && result) {
    return (
      <div className="site-shell">
        <SiteHeader />
        <main className="page-container page-container--auth">
          <div className="register-card success-card">
            <p className="section-kicker">已进入 AI 小说创作台</p>
            <h1>欢迎，{result.accountName}</h1>
            <p className="success-copy">
              你的旅程已开启。新手初光 <strong>{result.grantedLight}</strong> 微光已存入钱包。
            </p>
            <div className="register-actions">
              <Link className="button primary" href="/journey">开始我的旅程</Link>
              <Link className="button secondary" href="/">回到首页</Link>
            </div>
          </div>
        </main>
        <MobileNav />
      </div>
    );
  }

  return (
    <div className="site-shell">
      <SiteHeader />
      <main className="page-container page-container--auth">
        <div className="register-card">
          <div className="register-header">
            <Link className="brand-link" href="/">AI 小说创作台</Link>
            <p className="section-kicker">创建你的第一个世界</p>
            <h1>注册账号</h1>
            <p className="register-subtitle">用账号、密码和唯一邮箱进入一个尚未发生的世界。</p>
          </div>

          {error && <p className="register-error" role="alert">{error}</p>}

          <form className="register-form" onSubmit={submit} noValidate>
            <label className="field">
              <span className="field-label">账号名</span>
              <input
                type="text"
                value={accountName}
                onChange={(event) => setAccountName(event.target.value)}
                autoComplete="username"
                maxLength={20}
                aria-invalid={Boolean(fieldErrors.accountName)}
              />
              {fieldErrors.accountName && <span className="field-error">{fieldErrors.accountName}</span>}
            </label>

          <label className="field">
            <span className="field-label">密码</span>
            <input
              type="password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              autoComplete="new-password"
              aria-invalid={Boolean(fieldErrors.password)}
            />
            {fieldErrors.password && <span className="field-error">{fieldErrors.password}</span>}
          </label>

          <label className="field">
            <span className="field-label">邮箱</span>
            <input
              type="email"
              value={email}
              onChange={(event) => setEmail(event.target.value)}
              autoComplete="email"
              maxLength={320}
              aria-invalid={Boolean(fieldErrors.email)}
            />
            {fieldErrors.email && <span className="field-error">{fieldErrors.email}</span>}
          </label>

          <label className="field">
            <span className="field-label">邮箱验证码</span>
            <div className="code-row">
              <input
                type="text"
                value={code}
                onChange={(event) => setCode(event.target.value)}
                placeholder="请输入验证码"
                maxLength={40}
              />
              <button
                type="button"
                className="button secondary code-button"
                onClick={sendCode}
                disabled={sending || countdown > 0}
              >
                {sending ? "发送中…" : countdown > 0 ? `${countdown}s 后重发` : "发送验证码"}
              </button>
            </div>
            {echoedCode && (
              <p className="dev-code-hint">
                开发环境验证码已回显：<strong>{echoedCode}</strong>（生产环境会通过邮件发送）
              </p>
            )}
          </label>

          <label className="confirm-row">
            <input
              type="checkbox"
              checked={adultConfirmed}
              onChange={(event) => setAdultConfirmed(event.target.checked)}
              aria-invalid={Boolean(fieldErrors.adult)}
            />
            <span>
              我确认已满 <strong>18 周岁</strong>，并同意 AI 小说创作台相关条款与内容准则。
            </span>
          </label>
          {fieldErrors.adult && <span className="field-error">{fieldErrors.adult}</span>}

          {submitting ? (
            <span className="submit-working" role="status"><LogoStatePanel state="portal" copies={LOGO_STATE_COPY.portal} size={26} intervalMs={3000} /></span>
          ) : (
            <button className="button primary submit-button" type="submit">创建账号</button>
          )}
          </form>

          <p className="register-login">
            已有账号？<Link href="/login">登录</Link>
          </p>
        </div>
      </main>
      <MobileNav />
    </div>
  );
}
