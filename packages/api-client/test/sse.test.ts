import { describe, expect, it } from "vitest";
import { createSseParser, parseSseEvent } from "../src/index.js";

describe("SSE parsing", () => {
  it("parses event metadata and JSON data", () => {
    expect(
      parseSseEvent('id: evt-7\nevent: committed\nretry: 1500\ndata: {"jobId":"job-1","seq":2}\n'),
    ).toEqual({
      event: "committed",
      id: "evt-7",
      retry: 1500,
      rawData: '{"jobId":"job-1","seq":2}',
      data: { jobId: "job-1", seq: 2 },
    });
  });

  it("joins multiline data and preserves non-JSON payloads", () => {
    expect(parseSseEvent("event: progress\r\ndata: first\r\ndata: second\r\n")?.data).toBe(
      "first\nsecond",
    );
  });

  it("parses events split across arbitrary chunks", () => {
    const parser = createSseParser<{ stage: string }>();

    expect(parser.push("event: stage\ndata: {\"sta")).toEqual([]);
    expect(parser.push('ge":"writing"}\n\n: keepalive\n\nid: 2\ndata: done')).toEqual([
      {
        event: "stage",
        data: { stage: "writing" },
        rawData: '{"stage":"writing"}',
        id: undefined,
        retry: undefined,
      },
    ]);
    expect(parser.finish()).toEqual([
      {
        event: "message",
        data: "done",
        rawData: "done",
        id: "2",
        retry: undefined,
      },
    ]);
  });

  it("handles a CRLF event boundary split between chunks", () => {
    const parser = createSseParser();
    expect(parser.push("data: one\r")).toEqual([]);
    expect(parser.push("\n\r\ndata: two\r\n\r\n").map((event) => event.data)).toEqual([
      "one",
      "two",
    ]);
  });
});
