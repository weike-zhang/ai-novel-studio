// 人气展示：根据真实进入次数映射为人气等级，避免暴露原始数据。
export type PopularityLevel = "new" | "rising" | "popular" | "hot" | "burning";

export interface PopularityLabel {
  level: PopularityLevel;
  label: string;
}

export function popularityLabel(simulationCount: number): PopularityLabel {
  if (simulationCount <= 0) return { level: "new", label: "新作首发" };
  if (simulationCount < 3) return { level: "rising", label: "人气渐起" };
  if (simulationCount < 10) return { level: "popular", label: "人气上升" };
  if (simulationCount < 30) return { level: "hot", label: "日渐热门" };
  return { level: "burning", label: "人气火热" };
}
