import {
  IconArrowDown, IconArrowUp, IconFileText, IconLockFilled, IconRoute, IconServer, IconWifi,
} from '@tabler/icons-react';
import type { ConnectionDashboardState } from '../../lib/stores/connectionStore';

function formatBytes(value: number) {
  if (value <= 0) return '—';
  if (value < 1024) return `${value} Б`;
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} КБ`;
  if (value < 1024 * 1024 * 1024) return `${(value / 1024 / 1024).toFixed(1)} МБ`;
  return `${(value / 1024 / 1024 / 1024).toFixed(2)} ГБ`;
}

function latencyLabel(value: number | null | undefined) {
  if (value === undefined) return '…';
  if (value === null) return '—';
  return `${value} мс`;
}

export default function ConnectionStats({
  connection,
  latency,
  onOpenLogs,
}: {
  connection: ConnectionDashboardState;
  latency: number | null | undefined;
  onOpenLogs: () => void;
}) {
  const connected = connection.state === 'connected';

  return (
    <>
      <section className="connection-stats">
        <div className="stat-item">
          <IconWifi size={20} stroke={1.8} />
          <span>Пинг</span>
          <strong>{latencyLabel(latency)}</strong>
        </div>
        <div className="stat-item">
          <IconServer size={20} stroke={1.8} />
          <span>Активные воркеры</span>
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
          <IconRoute size={20} stroke={1.8} />
          <span>Маршрут</span>
          <strong>{connected ? 'IPv4 ✓ · IPv6 блок' : 'Ожидание'}</strong>
        </div>
      </section>

      <section className="connection-network-info" aria-label="Состояние сети">
        <div className="network-info-label">
          <span>Сеть</span>
        </div>
        <div>
          <IconServer size={18} stroke={1.8} />
          <span>
            <strong>IPv4</strong>
            <small>{connected ? 'через туннель' : 'ожидание подключения'}</small>
          </span>
        </div>
        <div>
          <IconLockFilled size={18} />
          <span>
            <strong>IPv6</strong>
            <small>{connected ? 'заблокирован' : 'защита включится при подключении'}</small>
          </span>
        </div>
        <button type="button" onClick={onOpenLogs}>
          <IconFileText size={18} stroke={1.8} />
          Открыть логи
        </button>
      </section>
    </>
  );
}
