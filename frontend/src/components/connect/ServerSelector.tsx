import type { MouseEvent } from 'react';
import { IconChevronDown } from '@tabler/icons-react';
import type { Server } from '../../lib/types';
import ServerList from './ServerList';
import { ServerIcon, pingColor } from './ServerIcon';
import { formatHashCount, getServerHashCount, getServerWorkers } from './ServerListItem';

function latencyText(latency: number | null | undefined) {
  if (latency === undefined) return '…';
  if (latency === null) return '—';
  return `${latency} мс`;
}

export default function ServerSelector({
  servers,
  selected,
  listOpen,
  obfsMode,
  latencies,
  onToggleList,
  onSelect,
  onIconClick,
  onEdit,
}: {
  servers: Server[];
  selected: Server | null;
  listOpen: boolean;
  obfsMode: 'audio' | 'video';
  latencies: Record<string, number | null | undefined>;
  onToggleList: () => void;
  onSelect: (server: Server) => void;
  onIconClick: (event: MouseEvent<HTMLButtonElement>, server: Server) => void;
  onEdit: (server: Server) => void;
}) {
  const subtitle = selected
    ? `${selected.host} · ${obfsMode === 'video' ? 'Video' : 'Audio'} · ${getServerWorkers(selected)} воркеров · ${formatHashCount(getServerHashCount(selected))}`
    : 'Добавьте первый профиль';

  const latency = selected ? latencies[selected.id] : undefined;
  const latencyColor = latency != null ? pingColor(latency) : 'var(--text-4)';

  return (
    <div className="server-selector">
      <button
        type="button"
        className={`status-server${!selected ? ' status-server--empty' : ''}`}
        onClick={onToggleList}
        aria-expanded={listOpen}
      >
        <span className="status-server-icon"><ServerIcon iconKey={selected?.icon} size={24} /></span>
        <span className="status-server-copy">
          <strong>{selected?.name ?? 'Нет серверов'}</strong>
          <small>{subtitle}</small>
        </span>
        <span className="status-server-side">
          {selected && (
            <span className="status-server-ping" style={{ color: latencyColor }} title="ICMP-пинг до сервера">
              {latency != null && <span className="ping-dot" style={{ background: latencyColor }} />}
              {latencyText(latency)}
            </span>
          )}
          <IconChevronDown
            size={19}
            style={{ transform: listOpen ? 'rotate(180deg)' : 'rotate(0deg)', transition: 'transform .2s' }}
          />
        </span>
      </button>

      {listOpen && servers.length > 0 && (
        <ServerList
          servers={servers}
          selected={selected}
          obfsMode={obfsMode}
          latencies={latencies}
          onSelect={onSelect}
          onIconClick={onIconClick}
          onEdit={onEdit}
        />
      )}
    </div>
  );
}
