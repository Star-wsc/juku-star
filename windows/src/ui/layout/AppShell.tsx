import type { ReactNode } from "react";
import { Sidebar } from "./Sidebar";
import { TitleBar } from "./TitleBar";

interface Props {
  title: string;
  active: string;
  onSelect: (id: string) => void;
  serverLabel: string;
  accountName?: string | null;
  children: ReactNode;
}

export function AppShell({
  title,
  active,
  onSelect,
  serverLabel,
  accountName,
  children,
}: Props) {
  return (
    <div className="window-frame">
      <TitleBar title={title} />
      <div className="window-body">
        <Sidebar
          active={active}
          onSelect={onSelect}
          serverLabel={serverLabel}
          accountName={accountName}
        />
        <main className="content-area">{children}</main>
      </div>
    </div>
  );
}
