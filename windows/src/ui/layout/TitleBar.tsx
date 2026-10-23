import { useCallback, useState } from "react";
import { getCurrentWindow } from "@tauri-apps/api/window";
import "./TitleBar.css";

export function TitleBar({ title }: { title: string }) {
  const win = getCurrentWindow();
  const [maximized, setMaximized] = useState(false);

  const onMin = useCallback(() => void win.minimize(), [win]);
  const onMax = useCallback(async () => {
    const m = await win.isMaximized();
    if (m) await win.unmaximize();
    else await win.maximize();
    setMaximized(!m);
  }, [win]);
  const onClose = useCallback(() => void win.close(), [win]);

  return (
    <div data-tauri-drag-region className="titlebar">
      <div data-tauri-drag-region className="titlebar-text">
        {title}
      </div>
      <div className="win-controls">
        <button type="button" className="win-btn" onClick={onMin} aria-label="最小化" title="最小化">
          <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
            <rect x="0" y="4.5" width="10" height="1" fill="currentColor" />
          </svg>
        </button>
        <button type="button" className="win-btn" onClick={onMax} aria-label="最大化" title="最大化">
          {maximized ? (
            <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
              <rect x="1.5" y="0.5" width="7" height="7" fill="none" stroke="currentColor" strokeWidth="1" />
              <rect x="0" y="2.5" width="7" height="7" fill="none" stroke="currentColor" strokeWidth="1" />
            </svg>
          ) : (
            <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
              <rect x="0.5" y="0.5" width="9" height="9" fill="none" stroke="currentColor" strokeWidth="1" />
            </svg>
          )}
        </button>
        <button type="button" className="win-btn close" onClick={onClose} aria-label="关闭" title="关闭">
          <svg width="10" height="10" viewBox="0 0 10 10" aria-hidden="true">
            <path d="M0.5 0.5 L9.5 9.5 M9.5 0.5 L0.5 9.5" stroke="currentColor" strokeWidth="1.1" />
          </svg>
        </button>
      </div>
    </div>
  );
}
