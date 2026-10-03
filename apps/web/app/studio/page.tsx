import type { Metadata } from "next";

import StudioWorkspace from "./studio-workspace";
import "./studio.css";

export const metadata: Metadata = {
  title: "创作台｜AI 小说创作台",
  description: "创建、预览并准备发布你的互动小说世界。",
};

export default function StudioPage() {
  return <StudioWorkspace />;
}
