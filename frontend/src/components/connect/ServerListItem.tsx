import type { MouseEvent } from 'react';
import { IconPencil } from '@tabler/icons-react';
import type { Server } from '../../lib/types';
import { ServerIcon, pingColor } from './ServerIcon';

export function getServerHashCount(server: Server) {
  return (server.hashes ?? []).filter(hash => hash.trim()).length;
}

export function getServerWorkers(server: Server) {
  const hashes = getServerHashCount(server);
  let workers = server.power || Math.max(9, hashes * 9);
  workers = Math.max(9, Math.min(108, workers));
  return Math.floor(workers / 9) * 9;
}

export default function ServerListItem({
  server,
  selected,
  obfsMode,
  onSelect,
  onIconClick,
  onEdit,
}: {
  server: Server;
  selected: boolean;
  obfsMode: 'audio' | 'video';
  onSelect: () => void;
  onIconClick: (event: MouseEvent<HTMLButtonElement>) => void;
  onEdit: () => void;
}) {
  const ping = server.ping;
  const metadata = `${obfsMode === 'video' ? 'Video' : 'Audio'} · ${getServerWorkers(server)} воркеров · ${getServerHashCount(server)} хеша`;

  return (
    <div
      className={`server-item${selected ? ' server-item--active' : ''}`}
      role="button"
      tabIndex={0}
      onClick={onSelect}
      onKeyDown={event => { if (event.key === 'Enter' || event.key === ' ') onSelect(); }}
    >
      <button type="button" className="server-icon-btn" onClick={event => { event.stopPropagation(); onIconClick(event); }} aria-label="Выбрать иконку">
        <ServerIcon iconKey={server.icon} size={24} />
      </button>
      <div className="server-copy">
        <div className="server-title-row">
          <span className="server-name">{server.name}</span>
          <span className="server-ping" style={{ color: pingColor(ping) }}>
            <span className="ping-dot" style={{ background: pingColor(ping) }} />
            {ping != null ? `${ping} мс` : '—'}
          </span>
        </div>
        <span className="server-host">{server.host}</span>
        <span className="server-meta">{metadata}</span>
      </div>
      <button type="button" className="server-edit-btn" onClick={event => { event.stopPropagation(); onEdit(); }} aria-label="Редактировать сервер">
        <IconPencil size={16} stroke={2} />
      </button>
    </div>
  );
}
