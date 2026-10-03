import type { Metadata } from "next";

import WorldDetail from "./world-detail";
import "./world-detail.css";

export const metadata: Metadata = {
  title: "世界详情｜AI 小说创作台",
  description: "查看世界设定并开启一段独立旅程。",
};

export default async function WorldDetailPage({
  params,
}: {
  params: Promise<{ worldId: string }>;
}) {
  const { worldId } = await params;
  return <WorldDetail worldId={worldId} />;
}
