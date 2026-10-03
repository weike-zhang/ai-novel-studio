import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "AI 小说创作台｜AI 互动小说世界",
  description: "进入一个尚未发生的世界，在每一次选择之间，让故事继续。",
  icons: { icon: "/brand/novelstudio-mark.svg" },
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return (
    <html lang="zh-CN" data-scroll-behavior="smooth">
      <body>{children}</body>
    </html>
  );
}
