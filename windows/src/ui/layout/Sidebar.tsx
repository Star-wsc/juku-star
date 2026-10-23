import "./Sidebar.css";

export interface NavItem {
  id: string;
  label: string;
}

const NAV: NavItem[] = [
  { id: "library", label: "剧库" },
  { id: "following", label: "追剧" },
  { id: "download", label: "下载" },
  { id: "history", label: "历史" },
  { id: "settings", label: "设置" },
];

interface Props {
  active: string;
  onSelect: (id: string) => void;
  serverLabel: string;
  accountName?: string | null;
}

export function Sidebar({ active, onSelect, serverLabel, accountName }: Props) {
  return (
    <aside className="sidebar">
      <nav className="sidebar-nav">
        {NAV.map((item) => {
          const selected =
            active === item.id ||
            (active === "detail" && item.id === "library") ||
            (active === "player" && item.id === "library");
          return (
            <button
              key={item.id}
              type="button"
              className={`sidebar-item ${selected ? "selected" : ""}`}
              onClick={() => onSelect(item.id)}
            >
              {selected && <span className="sidebar-bar" />}
              <span className="sidebar-label">{item.label}</span>
            </button>
          );
        })}
      </nav>
      <div className="server-card">
        <div className="server-label">服务器</div>
        <div className="server-value" title={serverLabel}>
          {serverLabel}
        </div>
        {accountName ? (
          <div className="server-account" title={accountName}>
            {accountName}
          </div>
        ) : null}
      </div>
    </aside>
  );
}
