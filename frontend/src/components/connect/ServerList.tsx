import type { MouseEvent } from 'react';
import type { Server } from '../../lib/types';
import ServerListItem from './ServerListItem';

export default function ServerList({
  servers,
  selected,
  obfsMode,
  onSelect,
  onIconClick,
  onEdit,
}: {
  servers: Server[];
  selected: Server | null;
  obfsMode: 'audio' | 'video';
  onSelect: (server: Server) => void;
  onIconClick: (event: MouseEvent<HTMLButtonElement>, server: Server) => void;
  onEdit: (server: Server) => void;
}) {
  return (
    <div className="server-list">
      {servers.map(server => (
        <ServerListItem
          key={server.id}
          server={server}
          selected={server.id === selected?.id}
          obfsMode={obfsMode}
          onSelect={() => onSelect(server)}
          onIconClick={event => onIconClick(event, server)}
          onEdit={() => onEdit(server)}
        />
      ))}
    </div>
  );
}
