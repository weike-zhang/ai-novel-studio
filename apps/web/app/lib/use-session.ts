"use client";

import { useEffect, useMemo, useState } from "react";
import type { Me } from "@ai-novel-studio/api-client";
import { createApiClient } from "./api";

export type SessionState =
  | { status: "loading" }
  | { status: "authenticated"; me: Me }
  | { status: "anonymous" };

export function useSession(): SessionState {
  const api = useMemo(() => createApiClient(), []);
  const [state, setState] = useState<SessionState>({ status: "loading" });

  useEffect(() => {
    let cancelled = false;
    api.getMe()
      .then((me) => {
        if (!cancelled) setState({ status: "authenticated", me });
      })
      .catch(() => {
        if (!cancelled) setState({ status: "anonymous" });
      });
    return () => {
      cancelled = true;
    };
  }, [api]);

  return state;
}
