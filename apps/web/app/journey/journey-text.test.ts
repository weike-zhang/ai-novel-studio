import { describe, expect, it } from "vitest";
import { splitStoryParagraphs } from "./journey-text";

describe("story text layout", () => {
  it("turns a single generated block into readable paragraphs", () => {
    const text = "你按下回拨键，控制台上的指示灯一盏盏亮起。通讯舱里只有风扇的低鸣，舷窗外的尘埃带缓慢掠过。你盯着倒计时，手指停在控制台边缘。七分钟后，扬声器终于亮起。那个声音说，你终于回来了。VEGA 同时提示信号来源无法定位。三号气闸开始解锁。";

    const paragraphs = splitStoryParagraphs(text);

    expect(paragraphs.length).toBeGreaterThan(1);
    expect(paragraphs.every((paragraph) => paragraph.length <= 120)).toBe(true);
    expect(paragraphs.at(-1)).toContain("三号气闸");
  });

  it("preserves authored paragraph breaks", () => {
    expect(splitStoryParagraphs("第一段。\n\n第二段。\n第三段。")).toEqual(["第一段。", "第二段。", "第三段。"]);
  });
});
