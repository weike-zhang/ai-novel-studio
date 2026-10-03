"use client";

import { useEffect, useRef } from "react";

export function Dialog({ title, onClose, children }: { title: string; onClose: () => void; children: React.ReactNode }) {
  const closeRef = useRef<HTMLButtonElement>(null);
  useEffect(() => {
    closeRef.current?.focus();
  }, []);
  return (
    <div className="dialog-layer" role="presentation" onKeyDown={(event) => event.key === "Escape" && onClose()}>
      <button className="dialog-backdrop" type="button" aria-label="关闭" onClick={onClose} />
      <section className="dialog-panel" role="dialog" aria-modal="true" aria-label={title}>
        <header>
          <h2>{title}</h2>
          <button ref={closeRef} type="button" aria-label="关闭" onClick={onClose}>×</button>
        </header>
        {children}
      </section>
    </div>
  );
}