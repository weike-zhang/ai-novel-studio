import { describe, expect, it } from "vitest";
import { COVER_UPLOAD_LIMITS, COVER_UPLOAD_MESSAGES, coverCropRect } from "./cover-upload";

describe("cover upload limits", () => {
  it("targets a 16:9 homepage cover", () => {
    expect(COVER_UPLOAD_LIMITS.outputWidth / COVER_UPLOAD_LIMITS.outputHeight).toBe(16 / 9);
    expect(COVER_UPLOAD_LIMITS.minWidth).toBe(COVER_UPLOAD_LIMITS.outputWidth);
    expect(COVER_UPLOAD_LIMITS.minHeight).toBe(COVER_UPLOAD_LIMITS.outputHeight);
  });

  it("rejects non-image files with a clear message", () => {
    expect(COVER_UPLOAD_LIMITS.accept).not.toContain("image/gif");
    expect(COVER_UPLOAD_MESSAGES.type).toContain("JPG");
  });
});

describe("coverCropRect", () => {
  it("keeps the full height when the source is wider than 16:9", () => {
    const crop = coverCropRect(1920, 1080, 1280, 720);
    expect(crop).toEqual({ x: 0, y: 0, width: 1920, height: 1080 });
  });

  it("center-crops a portrait source to the 16:9 band", () => {
    const crop = coverCropRect(1080, 1920, 1280, 720);
    expect(crop.width).toBe(1080);
    expect(crop.height).toBe(608);
    expect(crop.x).toBe(0);
    expect(crop.y).toBe(656);
    expect(crop.width / crop.height).toBeCloseTo(16 / 9, 1);
  });

  it("crops a square source to the 16:9 band", () => {
    const crop = coverCropRect(1600, 1600, 1280, 720);
    expect(crop.width).toBe(1600);
    expect(crop.height).toBe(900);
    expect(crop.x).toBe(0);
    expect(crop.y).toBe(350);
  });

  it("crops a wide panoramic source to the middle band", () => {
    const crop = coverCropRect(3840, 1080, 1280, 720);
    expect(crop.width).toBe(1920);
    expect(crop.height).toBe(1080);
    expect(crop.x).toBe(960);
    expect(crop.y).toBe(0);
  });
});
