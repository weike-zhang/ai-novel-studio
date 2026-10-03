"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { ApiClient, ApiError, type WorldReportReason } from "@ai-novel-studio/api-client";
import { createApiClient } from "../../lib/api";

const reportReasons: WorldReportReason[] = ["违法违规", "色情低俗", "暴力仇恨", "侵权冒用", "其他"];

function createWorldApi(): ApiClient {
  return createApiClient();
}

export default function ReportWorld({ worldId, worldTitle }: { worldId: string; worldTitle: string }) {
  const [isOpen, setIsOpen] = useState(false);
  const [reason, setReason] = useState<WorldReportReason | "">("");
  const [note, setNote] = useState("");
  const [message, setMessage] = useState("");
  const [isSubmitting, setIsSubmitting] = useState(false);
  const closeButtonRef = useRef<HTMLButtonElement>(null);
  const api = useMemo(() => createWorldApi(), []);

  useEffect(() => {
    if (isOpen) closeButtonRef.current?.focus();
  }, [isOpen]);

  function closeDialog() {
    setIsOpen(false);
    setMessage("");
  }

  async function submitReport(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (isSubmitting) return;
    if (!reason) {
      setMessage("请选择一个举报原因。当前不会提交任何内容。");
      return;
    }
    setIsSubmitting(true);
    setMessage("");
    try {
      const report = await api.reportWorld(worldId, { reason, note: note.trim() || undefined });
      setMessage(report.status === "pending" ? "举报已受理，我们会尽快核查。" : "举报已提交。");
      setReason("");
      setNote("");
      setIsOpen(false);
    } catch (error) {
      if (error instanceof ApiError) {
        if (error.status === 401) setMessage("举报需要先登录。");
        else if (error.status === 409) setMessage("你已举报过这个世界，正在处理中。");
        else if (error.status === 404) setMessage("这个世界不存在或已被下架。");
        else setMessage(error.message || "举报提交失败，请稍后重试。");
      } else {
        setMessage("举报提交失败，请稍后重试。");
      }
    } finally {
      setIsSubmitting(false);
    }
  }

  return (
    <>
      <button className="report-trigger" type="button" onClick={() => setIsOpen(true)}>
        举报这个世界
      </button>

      {isOpen && (
        <div className="dialog-layer" role="presentation" onKeyDown={(event) => event.key === "Escape" && closeDialog()}>
          <button className="dialog-backdrop" type="button" aria-label="关闭举报窗口" onClick={closeDialog} />
          <section className="dialog-panel" role="dialog" aria-modal="true" aria-labelledby="report-title">
            <header>
              <h2 id="report-title">举报《{worldTitle}》</h2>
              <button ref={closeButtonRef} type="button" aria-label="关闭" onClick={closeDialog}>×</button>
            </header>
            <form onSubmit={(event) => void submitReport(event)}>
              <label htmlFor="report-reason">举报原因</label>
              <select id="report-reason" value={reason} onChange={(event) => setReason(event.target.value as WorldReportReason | "")}>
                <option value="">请选择</option>
                {reportReasons.map((item) => <option key={item}>{item}</option>)}
              </select>
              <label htmlFor="report-note">补充说明 <span>选填</span></label>
              <textarea id="report-note" maxLength={300} value={note} onChange={(event) => setNote(event.target.value)} placeholder="请描述需要核查的内容" />
              <div className="report-helper">{note.length}/300 · 举报需要先登录</div>
              {message && <p className="report-message" role="status">{message}</p>}
              <button className="report-submit" type="submit" disabled={isSubmitting}>{isSubmitting ? "提交中…" : "提交举报"}</button>
            </form>
          </section>
        </div>
      )}
    </>
  );
}
