import type { MouseEvent } from 'react';
import { IconPencil } from '@tabler/icons-react';
import type { Server } from '../../lib/types';
import { ServerIcon, pingColor } from './ServerIcon';

export function getServerHashCount(server: Server) {
  return (server.hashes ?? []).filter(hash => hash.trim()).length;
}

export function formatHashCount(count: number) {
  const mod10 = count % 10;
  const mod100 = count % 100;
  if (mod10 === 1 && mod100 !== 11) return `${count} хеш`;
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return `${count} хеша`;
  return `${count} хешей`;
}

export function getServerWorkers(server: Server) {
  const hashes = getServerHashCount(server);
  let workers = server.power || Math.max(9, hashes * 9);
  workers = Math.max(9, Math.min(108, workers));
  return Math.floor(workers / 9) * 9;
}

function latencyText(latency: number | null | undefined) {
  if (latency === undefined) return '…';
  if (latency === null) return '—';
  return `${latency} мс`;
}

export default function ServerListItem({
  server,
  selected,
  obfsMode,
  latency,
  onSelect,
  onIconClick,
  onEdit,
}: {
  server: Server;
  selected: boolean;
  obfsMode: 'audio' | 'video';
  latency: number | null | undefined;
  onSelect: () => void;
  onIconClick: (event: MouseEvent<HTMLButtonElement>) => void;
  onEdit: () => void;
}) {
  const metadata = `${obfsMode === 'video' ? 'Video' : 'Audio'} · ${getServerWorkers(server)} воркеров · ${formatHashCount(getServerHashCount(server))}`;
  const latencyColor = latency != null ? pingColor(latency) : 'var(--text-4)';

  return (
    <div
      className={`server-item${selected ? ' server-item--active' : ''}`}
      role="button"
      tabIndex={0}
      onClick={onSelect}
      onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') onSelect(); }}
    >
      <button
        type="button"
        className="server-icon-btn"
        onClick={event => { event.stopPropagation(); onIconClick(event); }}
        aria-label="Выбрать иконку"
      >
        <ServerIcon iconKey={server.icon} size={22} />
      </button>

      <div className="server-copy">
        <span className="server-name">{server.name}</span>
        <div className="server-details">
          <span className="server-host">{server.host}</span>
          <span className="server-detail-separator">•</span>
          <span className="server-meta">{metadata}</span>
        </div>
      </div>

      <div className="server-side">
        <span className="server-ping" style={{ color: latencyColor }} title="ICMP-пинг до сервера">
          {latency != null && <span className="ping-dot" style={{ background: latencyColor }} />}
          {latencyText(latency)}
        </span>
        <button
          type="button"
          className="server-edit-btn"
          onClick={event => { event.stopPropagation(); onEdit(); }}
          aria-label="Редактировать сервер"
        >
          <IconPencil size={16} stroke={2} />
        </button>
      </div>
    </div>
  );
}
