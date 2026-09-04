import { useEffect, useState } from 'react';
import type { Server } from '../../lib/types';
import type { ConnectionDashboardState } from '../../lib/stores/connectionStore';
import shapeLight from '../../assets/shape-light.png';
import shapeDark from '../../assets/shape-dark.png';
import powerIcon from '../../assets/power-icon.png';

function formatDuration(startedAt: number | null, now: number) {
  if (!startedAt) return '00:00:00';
  const total = Math.max(0, Math.floor((now - startedAt) / 1000));
  const h = Math.floor(total / 3600).toString().padStart(2, '0');
  const m = Math.floor((total % 3600) / 60).toString().padStart(2, '0');
  const s = (total % 60).toString().padStart(2, '0');
  return `${h}:${m}:${s}`;
}

export default function ConnectionHero({
  theme,
  connection,
  selected,
  linkFlash,
  onTunnel,
  onRetry,
  onLogs,
}: {
  theme: string;
  connection: ConnectionDashboardState;
  selected: Server | null;
  linkFlash: boolean;
  onTunnel: () => void;
  onRetry: () => void;
  onLogs: () => void;
}) {
  const [now, setNow] = useState(Date.now());

  useEffect(() => {
    if (connection.state !== 'connected') return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [connection.state]);

  const hasError = connection.state === 'idle' && Boolean(connection.lastError);
  const title = !selected
    ? 'Добавьте сервер'
    : hasError
      ? 'Не удалось подключиться'
      : connection.state === 'connected'
        ? 'Подключено'
        : connection.state === 'connecting'
          ? 'Подключение к серверу'
          : connection.state === 'disconnecting'
            ? 'Отключение'
            : 'Готово к подключению';

  const subtitle = !selected
    ? 'Создайте профиль сервера для подключения'
    : hasError
      ? connection.lastError ?? ''
      : connection.state === 'connected'
        ? 'Туннель работает'
        : connection.state === 'connecting'
          ? (connection.message || 'Подготавливаем защищённый туннель')
          : connection.state === 'disconnecting'
            ? 'Завершаем сетевые сессии'
            : `${selected.name} · нажмите кнопку для подключения`;

  return (
    <section className="connection-hero">
      <button
        type="button"
        className={`power-btn power-btn--${connection.state}${hasError ? ' power-btn--error' : ''}`}
        onClick={onTunnel}
        disabled={!selected || connection.state === 'disconnecting'}
        aria-label={connection.state === 'connected' || connection.state === 'connecting' ? 'Отключить' : 'Подключить'}
      >
        <div className={`orb${connection.state === 'connecting' ? ' orb--spinning' : ''}${connection.state === 'connected' ? ' orb--active' : ''}${linkFlash ? ' orb--flash' : ''}`}>
          <img src={theme === 'dark' ? shapeLight : shapeDark} alt="" draggable={false} />
        </div>
        <div className="power-icon">
          <img src={powerIcon} alt="" draggable={false} />
        </div>
      </button>

      <div className={`hero-status${hasError ? ' hero-status--error' : ''}`}>
        <strong>{title}</strong>
        <span>{subtitle}</span>
        {connection.state === 'connected' && (
          <time>{formatDuration(connection.connectedAt, now)}</time>
        )}
        {hasError && selected && (
          <div className="hero-actions">
            <button type="button" onClick={onRetry}>Повторить</button>
            <button type="button" className="hero-action-secondary" onClick={onLogs}>Логи</button>
          </div>
        )}
      </div>
    </section>
  );
}
