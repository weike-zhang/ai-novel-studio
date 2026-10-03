"use client";

import { useEffect, useState } from "react";

// AI 小说创作台 Logo 状态动效 —— 动效语言「停留—选择—穿过界门」。
// 基于 docs/assets/novelstudio-logo-states.html 的 12 个状态移植。
// Logo 不做宠物化动作：克制、有氛围、只表达状态。

export type LogoState =
  | "idle"        // 待机：界门缓慢呼吸，境点明灭
  | "hover"       // 悬停：界门微启、整体浮起
  | "press"       // 点击：预备—压实—弹起
  | "loading"     // 加载：境点穿过界门，留下拖尾
  | "busy"        // 忙碌：急促呼吸，境点起伏
  | "success"     // 成功：界门敞开，苔绿光芒，光环荡开
  | "failure"     // 失败：界门合拢，境点被拒退回，红光震荡
  | "focus"       // 聚焦：陶土色焦点环
  | "disabled"    // 禁用：降灰降透明，无动效
  | "notification" // 通知：右上红点轻跳
  | "decision"    // 抉择：左右选项间权衡
  | "portal";     // 穿越：镜头推进，没入白光

export const LOGO_STATES: LogoState[] = [
  "idle", "hover", "press", "loading", "busy", "success",
  "failure", "focus", "disabled", "notification", "decision", "portal",
];

// 每个状态的多条品牌文案，随展示轮播。
export const LOGO_STATE_COPY: Record<LogoState, string[]> = {
  idle: [
    "界门半开，静候你的一次选择。",
    "两个世界之间，等你落下一步。",
    "门后的世界没有催促，只等你说出想去的方向。",
  ],
  hover: [
    "这道门，嗅到了你的目光。",
    "靠近一点，门后的光随之亮起。",
  ],
  press: [
    "按下这扇门，故事会替你推开。",
    "你按下的不是按钮，是世界的开关。",
  ],
  loading: [
    "境点正穿过界门，故事在另一侧成形。",
    "门后正在展开一个新的世界。",
    "下一幕，正在界门之后生长。",
  ],
  busy: [
    "界门正在磨出新的缝隙。",
    "世界还在校准，稍候片刻。",
    "门后灯火未熄，故事仍在赶路。",
  ],
  success: [
    "门已敞开，你抵达了新的世界。",
    "界门之后，故事为你亮起。",
    "这一章写就，世界向前一步。",
  ],
  failure: [
    "界门暂时合拢，故事被挡在门外。",
    "这条岔路没有打开，微光已退回你的手中。",
    "门后起了雾，这一程没能抵达。",
  ],
  focus: [
    "界门已就绪，等你确认。",
    "境点亮起，这一处需要你落定。",
  ],
  disabled: [
    "这扇门此刻未为你开启。",
    "界门对当下的你闭合着。",
  ],
  notification: [
    "门后传来一个消息。",
    "界门另一端，有人等你回应。",
    "有一件事，正在门外等着你。",
  ],
  decision: [
    "两个世界摆在门前，选一条路进去。",
    "向左，还是向右？界门在等你的决定。",
    "这一次选择，将决定你落在哪个世界。",
  ],
  portal: [
    "穿过界门。",
    "正在进入门后的世界。",
    "白光漫过，新的世界正在接住你。",
  ],
};

// 状态默认的焦点元素（用于简化挂接）
export function logoStateForJourney(status: string): LogoState {
  switch (status) {
    case "queued": return "busy";
    case "generating": return "loading";
    case "saving": return "busy";
    case "failed": return "failure";
    case "stopped": return "idle";
    default: return "idle";
  }
}

export interface LogoStateMarkProps {
  state: LogoState;
  size?: number;
  theme?: "default" | "reverse";
  className?: string;
  ariaLabel?: string;
}

export function LogoStateMark({ state, size = 56, theme = "default", className, ariaLabel }: LogoStateMarkProps) {
  return (
    <svg
      className={`logo-state-mark logo-state-${state} ${theme === "reverse" ? "theme-reverse" : ""} ${className ?? ""}`}
      viewBox="0 0 256 256"
      width={size}
      height={size}
      role="img"
      aria-hidden={ariaLabel ? undefined : true}
      aria-label={ariaLabel}
      focusable="false"
    >
      <defs>
        <radialGradient id="jj-glow-success" cx="50%" cy="50%" r="50%">
          <stop offset="0%" stopColor="#B9D0C0" />
          <stop offset="100%" stopColor="#E5ECE8" stopOpacity="0" />
        </radialGradient>
        <radialGradient id="jj-glow-danger" cx="50%" cy="50%" r="50%">
          <stop offset="0%" stopColor="#E8B3AC" />
          <stop offset="100%" stopColor="#F4E1DF" stopOpacity="0" />
        </radialGradient>
        <radialGradient id="jj-glow-portal" cx="50%" cy="50%" r="50%">
          <stop offset="0%" stopColor="#FFFFFF" />
          <stop offset="55%" stopColor="#F1DED6" />
          <stop offset="100%" stopColor="#F1DED6" stopOpacity="0" />
        </radialGradient>
      </defs>
      <g className="glow"><ellipse className="glow-shape" cx="129" cy="129" rx="34" ry="44" fill="url(#jj-glow-success)" /></g>
      <g className="portal-glow"><ellipse className="portal-shape" cx="129" cy="129" rx="40" ry="52" fill="url(#jj-glow-portal)" /></g>
      <g className="ring"><circle className="ring-shape" cx="129" cy="129" r="34" fill="none" /></g>
      <g className="opt opt-l"><circle className="opt-shape" cx="96" cy="129" r="9" /></g>
      <g className="opt opt-r"><circle className="opt-shape" cx="162" cy="129" r="9" /></g>
      <g className="badge"><circle className="badge-shape" cx="198" cy="46" r="17" /></g>
      <g className="plane plane-left"><path className="plane-path" d="M36 40 112 82v92l-76 42V40Z" /></g>
      <g className="plane plane-right"><path className="plane-path" d="M220 58 144 100v56l76 42V58Z" /></g>
      <g className="trail"><rect className="trail-rect" x="124" y="124" width="10" height="10" rx="2" /></g>
      <g className="point"><rect className="point-rect" x="124" y="124" width="10" height="10" rx="2" /></g>
    </svg>
  );
}

// 文案轮播：按 intervalMs 间隔在多条文案间切换。
// 单条文案时保持静止。
export function useRotatingCopy(copies: string[], intervalMs = 6000): string {
  const [index, setIndex] = useState(0);
  useEffect(() => {
    if (copies.length <= 1) return;
    const timer = window.setInterval(() => {
      setIndex((current) => (current + 1) % copies.length);
    }, intervalMs);
    return () => window.clearInterval(timer);
  }, [copies.length, intervalMs]);
  return copies[Math.min(index, copies.length - 1)];
}

// 状态条组合：Logo 动效 + 品牌文案（自动轮播）
export function LogoStatePanel({
  state,
  copies,
  size = 40,
  intervalMs,
  theme,
  className,
}: {
  state: LogoState;
  copies: string[];
  size?: number;
  intervalMs?: number;
  theme?: "default" | "reverse";
  className?: string;
}) {
  const copy = useRotatingCopy(copies, intervalMs);
  return (
    <span className={`logo-state-panel logo-state-panel-${state} ${theme === "reverse" ? "theme-reverse" : ""} ${className ?? ""}`} role="status" aria-live="polite">
      <LogoStateMark state={state} size={size} theme={theme} />
      <span className="logo-state-copy">{copy}</span>
    </span>
  );
}
