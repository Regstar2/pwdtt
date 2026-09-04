import {
  IconArrowDown, IconArrowUp, IconFileText, IconGlobe, IconLockFilled, IconServer, IconWifi,
} from '@tabler/icons-react';
import type { Server } from '../../lib/types';
import type { ConnectionDashboardState } from '../../lib/stores/connectionStore';

function formatBytes(value: number) {
  if (value <= 0) return '—';
  if (value < 1024) return `${value} Б`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} КБ`;
  if (value < 1024 * 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} МБ`;
  return `${(value / 1024 / 1024 / 1024).toFixed(2)} ГБ`;
}

export default function ConnectionStats({
  connection,
  selected,
  onOpenLogs,
}: {
  connection: ConnectionDashboardState;
  selected: Server | null;
  onOpenLogs: () => void;
}) {
  const connected = connection.state === 'connected';

  return (
    <>
      <section className="connection-stats">
        <div className="stat-item">
          <IconWifi size={20} stroke={1.8} />
          <span>Пинг</span>
          <strong>{selected?.ping != null ? `${selected.ping} мс` : '—'}</strong>
        </div>
        <div className="stat-item">
          <IconServer size={20} stroke={1.8} />
          <span>Активных воркеров</span>
          <strong>{connection.totalWorkers > 0 ? `${connection.activeWorkers} / ${connection.totalWorkers}` : '— / —'}</strong>
        </div>
        <div className="stat-item">
          <IconArrowDown size={20} stroke={1.8} />
          <span>Загрузка</span>
          <strong>{formatBytes(connection.bytesDown)}</strong>
        </div>
        <div className="stat-item">
          <IconArrowUp size={20} stroke={1.8} />
          <span>Отдача</span>
          <strong>{formatBytes(connection.bytesUp)}</strong>
        </div>
        <div className="stat-item">
          <IconGlobe size={20} stroke={1.8} />
          <span>IPv6</span>
          <strong>{connected ? 'Заблокирован' : '—'}</strong>
        </div>
      </section>

      {connected && (
        <section className="connection-network-info">
          <div>
            <IconServer size={18} stroke={1.8} />
            <span><strong>IPv4</strong><small>через туннель</small></span>
          </div>
          <div>
            <IconLockFilled size={18} />
            <span><strong>IPv6</strong><small>заблокирован</small></span>
          </div>
          <button type="button" onClick={onOpenLogs}>
            <IconFileText size={18} stroke={1.8} />
            Открыть логи
          </button>
        </section>
      )}
    </>
  );
}
