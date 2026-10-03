"use client";

import Image from "next/image";
import Link from "next/link";
import { usePathname } from "next/navigation";
import { useSession } from "../lib/use-session";

export function Glimmer({ value }: { value: number }) {
  return <span className="glimmer"><i /> {value} 微光</span>;
}

export function AuthMenu() {
  const session = useSession();
  const pathname = usePathname();

  if (session.status === "loading") {
    return <span className="glimmer"><i /> 载入中…</span>;
  }
  if (session.status === "anonymous") {
    return (
      <div className="app-header-actions">
        <Link className="button secondary small" href={`/login?next=${encodeURIComponent(pathname)}`}>登录</Link>
        <Link className="button primary small" href="/register">注册</Link>
      </div>
    );
  }
  return (
    <div className="app-header-actions">
      <Glimmer value={session.me.availableBalance} />
      <Link className="avatar" href="/account" aria-label="打开账户设置">
        {session.me.nickname || session.me.accountName.slice(0, 1)}
      </Link>
    </div>
  );
}

const navItems = [
  { href: "/", label: "世界广场" },
  { href: "/studio", label: "创作" },
  { href: "/journey", label: "旅程" },
  { href: "/account", label: "我的" },
];

export function SiteHeader() {
  const pathname = usePathname();
  const isActive = (href: string) => {
    if (href === "/") return pathname === "/";
    return pathname === href || pathname.startsWith(`${href}/`);
  };

  return (
    <header className="app-header">
      <div className="app-header-inner">
        <Link className="app-brand" href="/" aria-label="AI 小说创作台首页">
          <Image src="/brand/novelstudio-logo-horizontal.svg" width={176} height={40} alt="AI 小说创作台" priority />
        </Link>
        <nav className="app-nav" aria-label="主导航">
          {navItems.map((item) => (
            <Link
              key={item.href}
              href={item.href}
              className={isActive(item.href) ? "active" : ""}
              aria-current={isActive(item.href) ? "page" : undefined}
            >
              {item.label}
            </Link>
          ))}
        </nav>
        <AuthMenu />
      </div>
    </header>
  );
}
