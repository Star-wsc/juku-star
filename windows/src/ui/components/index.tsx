import type { ReactNode } from "react";
import { coverCandidates, nextCoverFallback, useCoverUrl } from "../../core/useCoverUrl";
import "./components.css";

export function Chip({
  selected,
  onClick,
  children,
}: {
  selected?: boolean;
  onClick?: () => void;
  children: ReactNode;
}) {
  return (
    <button type="button" className={`chip ${selected ? "selected" : ""}`} onClick={onClick}>
      {children}
    </button>
  );
}

export function SwitchRow({
  title,
  description,
  checked,
  onChange,
  disabled,
  extra,
}: {
  title: string;
  description?: string;
  checked: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  extra?: ReactNode;
}) {
  return (
    <div
      className={`switch-row ${disabled ? "disabled" : ""}`}
      onClick={() => {
        if (!disabled) onChange(!checked);
      }}
    >
      <div className="switch-text">
        <div className="switch-title">{title}</div>
        {description ? <div className="switch-desc">{description}</div> : null}
      </div>
      {extra ? <div className="switch-extra">{extra}</div> : null}
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-label={title}
        className={`switch ${checked ? "on" : "off"}`}
        disabled={disabled}
        onClick={(e) => {
          e.stopPropagation();
          if (!disabled) onChange(!checked);
        }}
      >
        <span className="knob" />
      </button>
    </div>
  );
}

export function PosterCard({
  title,
  subtitle,
  cover,
  badge,
  onClick,
}: {
  title: string;
  subtitle?: string;
  cover: string;
  badge?: string;
  onClick?: () => void;
}) {
  const src = useCoverUrl(cover);
  return (
    <button type="button" className="poster-card" onClick={onClick}>
      <div className="poster-cover">
        {src ? (
          <img
            src={src}
            alt={title}
            loading="lazy"
            draggable={false}
            onError={(e) => {
              const img = e.currentTarget;
              const tried = (img.dataset.tried || "").split("|").filter(Boolean);
              tried.push(img.src);
              img.dataset.tried = tried.join("|");
              void nextCoverFallback(cover, tried).then((next) => {
                if (next) img.src = next;
                else {
                  img.style.display = "none";
                  img.parentElement?.classList.add("show-fallback");
                }
              });
            }}
          />
        ) : null}
        <div className={`poster-fallback ${src ? "poster-fallback-under" : ""}`}>
          <span className="poster-fallback-text">{title.slice(0, 4)}</span>
        </div>
        {badge ? <span className="poster-badge">{badge}</span> : null}
      </div>
      <div className="poster-meta">
        <div className="poster-title" title={title}>
          {title}
        </div>
        {subtitle ? <div className="poster-sub">{subtitle}</div> : null}
      </div>
    </button>
  );
}

export function EmptyState({
  icon = "◌",
  title,
  description,
  action,
}: {
  icon?: string;
  title: string;
  description?: string;
  action?: ReactNode;
}) {
  return (
    <div className="empty-state">
      <div className="empty-icon">{icon}</div>
      <div style={{ fontSize: 15, fontWeight: 600, color: "var(--text)" }}>{title}</div>
      {description ? <div className="t-caption">{description}</div> : null}
      {action}
    </div>
  );
}

export { coverCandidates };
