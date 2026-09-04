import type { MouseEvent } from 'react';
import { IconChevronUp } from '@tabler/icons-react';
import type { Server } from '../../lib/types';
import ServerList from './ServerList';
import { ServerIcon, pingColor } from './ServerIcon';
import { formatHashCount, getServerHashCount, getServerWorkers } from './ServerListItem';

export default function ServerSelector({
  servers,
  selected,
  listOpen,
  obfsMode,
  onToggleList,
  onSelect,
  onIconClick,
  onEdit,
}: {
  servers: Server[];
  selected: Server | null;
  listOpen: boolean;
  obfsMode: 'audio' | 'video';
  onToggleList: () => void;
  onSelect: (server: Server) => void;
  onIconClick: (event: MouseEvent<HTMLButtonElement>, server: Server) => void;
  onEdit: (server: Server) => void;
}) {
  const subtitle = selected
    ? `${obfsMode === 'video' ? 'Video' : 'Audio'} · ${getServerWorkers(selected)} воркеров · ${formatHashCount(getServerHashCount(selected))}`
    : 'Добавьте первый профиль';

  return (
    <div className="server-selector">
      {listOpen && servers.length > 0 && (
        <ServerList
          servers={servers}
          selected={selected}
          obfsMode={obfsMode}
          onSelect={onSelect}
          onIconClick={onIconClick}
          onEdit={onEdit}
        />
      )}

      <button type="button" className={`status-server${!selected ? ' status-server--empty' : ''}`} onClick={onToggleList}>
        <span className="status-server-icon"><ServerIcon iconKey={selected?.icon} size={23} /></span>
        <span className="status-server-copy">
          <strong>{selected?.name ?? 'Нет серверов'}</strong>
          <small>{subtitle}</small>
        </span>
        <span className="status-server-side">
          {selected?.ping != null && (
            <span className="status-server-ping" style={{ color: pingColor(selected.ping) }} title="Задержка до сервера">
              <span className="ping-dot" style={{ background: pingColor(selected.ping) }} />
              {selected.ping} мс
            </span>
          )}
          <IconChevronUp
            size={18}
            style={{ transform: listOpen ? 'rotate(0deg)' : 'rotate(180deg)', transition: 'transform 0.2s' }}
          />
        </span>
      </button>
    </div>
  );
}
