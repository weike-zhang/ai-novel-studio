import type { Metadata } from "next";

import JourneyExperience from "../journey-experience";
import "../journey.css";

export const metadata: Metadata = {
  title: "旅程｜AI 小说创作台",
  description: "继续你的私人旅程。",
};

// A minimal fallback used only before the real snapshot arrives from the API.
const journeyFallback = {
  simulationId: "",
  worldId: "",
  worldTitle: "旅程",
  turnLabel: "第 1 幕",
  chapterTitle: "旅程开场",
  savedAt: "刚刚",
  glimmer: 0,
  scene: [],
  quote: "",
  branches: [],
  scenes: [],
  state: {
    location: "",
    time: "",
    relationships: [],
    variables: [],
    echoes: [],
  },
};

export default async function JourneyPage({
  params,
}: {
  params: Promise<{ simulationId: string }>;
}) {
  const { simulationId } = await params;
  return <JourneyExperience journey={{ ...journeyFallback, simulationId }} />;
}
