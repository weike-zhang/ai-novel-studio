export interface SseEvent<T = unknown> {
  event: string;
  data: T | string;
  rawData: string;
  id?: string;
  retry?: number;
}

function decodeData<T>(rawData: string): T | string {
  if (rawData === "") return "";
  try {
    return JSON.parse(rawData) as T;
  } catch {
    return rawData;
  }
}

/** Parse one SSE event block. Comment-only/empty blocks return null. */
export function parseSseEvent<T = unknown>(block: string): SseEvent<T> | null {
  const dataLines: string[] = [];
  let event = "message";
  let id: string | undefined;
  let retry: number | undefined;
  let hasField = false;

  const normalized = block.replace(/^\uFEFF/, "").replace(/\r\n?/g, "\n");
  for (const line of normalized.split("\n")) {
    if (line === "" || line.startsWith(":")) continue;

    const colon = line.indexOf(":");
    const field = colon === -1 ? line : line.slice(0, colon);
    let value = colon === -1 ? "" : line.slice(colon + 1);
    if (value.startsWith(" ")) value = value.slice(1);

    switch (field) {
      case "event":
        event = value;
        hasField = true;
        break;
      case "data":
        dataLines.push(value);
        hasField = true;
        break;
      case "id":
        if (!value.includes("\0")) id = value;
        hasField = true;
        break;
      case "retry":
        if (/^\d+$/.test(value)) retry = Number(value);
        hasField = true;
        break;
    }
  }

  if (!hasField) return null;
  const rawData = dataLines.join("\n");
  return { event, data: decodeData<T>(rawData), rawData, id, retry };
}

export interface SseParser<T = unknown> {
  push(chunk: string): SseEvent<T>[];
  finish(): SseEvent<T>[];
  reset(): void;
}

/** Incrementally parse arbitrary text chunks from a fetch-based SSE stream. */
export function createSseParser<T = unknown>(): SseParser<T> {
  let buffer = "";

  const drain = (): SseEvent<T>[] => {
    const events: SseEvent<T>[] = [];
    let boundary: RegExpExecArray | null;
    while ((boundary = /\r\n\r\n|\n\n|\r\r/.exec(buffer)) !== null) {
      const block = buffer.slice(0, boundary.index);
      buffer = buffer.slice(boundary.index + boundary[0].length);
      const parsed = parseSseEvent<T>(block);
      if (parsed) events.push(parsed);
    }
    return events;
  };

  return {
    push(chunk) {
      buffer += chunk;
      return drain();
    },
    finish() {
      if (!buffer) return [];
      const finalBlock = buffer;
      buffer = "";
      const parsed = parseSseEvent<T>(finalBlock);
      return parsed ? [parsed] : [];
    },
    reset() {
      buffer = "";
    },
  };
}
