"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { Compass, Map, SquarePen, User } from "lucide-react";

const items = [
  { href: "/", label: "广场", Icon: Compass },
  { href: "/studio", label: "创作", Icon: SquarePen },
  { href: "/journey", label: "旅程", Icon: Map },
  { href: "/account", label: "我的", Icon: User },
];

export function MobileNav() {
  const pathname = usePathname();
  const isActive = (href: string) =>
    href === "/" ? pathname === "/" : pathname === href || pathname.startsWith(`${href}/`);

  return (
    <nav className="mobile-nav" aria-label="移动端导航">
      {items.map(({ href, label, Icon }) => (
        <Link
          key={href}
          href={href}
          className={isActive(href) ? "active" : ""}
          aria-current={isActive(href) ? "page" : undefined}
        >
          <span className="mobile-nav-icon" aria-hidden="true"><Icon size={22} strokeWidth={1.8} /></span>
          <span className="mobile-nav-label">{label}</span>
        </Link>
      ))}
    </nav>
  );
}
