import { useEffect, useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  IconAlertTriangle,
  IconCheck,
  IconChevronRight,
  IconCircle,
  IconLoader2,
  IconRefresh,
  IconRoute,
  IconShieldCheck,
  IconWifi,
} from '@tabler/icons-react';
import type { TunnelState } from '../lib/types';
import type { ConnectionDiagnostics, CheckState } from '../lib/connectionDiagnostics';
import './ConnectionStatus.css';

interface Props {
  serverName: string;
  ping?: number;
  configuredWorkers: number;
  tunnelState: TunnelState;
  diagnostics: ConnectionDiagnostics;
  onRetry: () => void;
}

function formatDuration(totalSeconds: number) {
  const h = Math.floor(totalSeconds / 3600);
  const m = Math.floor((totalSeconds % 3600) / 60);
  const s = totalSeconds % 60;
  return [h, m, s].map(v => String(v).padStart(2, '0')).join(':');
}

function CheckBadge({ state, okLabel }: { state: CheckState; okLabel: string }) {
  if (state === 'ok') return <span className="route-state route-state--ok"><IconCheck size={13} />{okLabel}</span>;
  if (state === 'error') return <span className="route-state route-state--error"><IconAlertTriangle size={13} />Проблема</span>;
  return <span className="route-state route-state--unknown">Нет данных</span>;
}

export default function ConnectionStatus({
  serverName,
  ping,
  configuredWorkers,
  tunnelState,
  diagnostics,
  onRetry,
}: Props) {
  const navigate = useNavigate();
  const [connectedAt, setConnectedAt] = useState<number | null>(null);
  const [seconds, setSeconds] = useState(0);

  useEffect(() => {
    if (tunnelState === 'connected') {
      setConnectedAt(prev => prev ?? Date.now());
      return;
    }
    setConnectedAt(null);
    setSeconds(0);
  }, [tunnelState]);

  useEffect(() => {
    if (connectedAt === null) return;
    const tick = () => setSeconds(Math.max(0, Math.floor((Date.now() - connectedAt) / 1000)));
    tick();
    const id = window.setInterval(tick, 1000);
    return () => window.clearInterval(id);
  }, [connectedAt]);

  const steps = useMemo(() => {
    const done = [
      diagnostics.serverReachable,
      diagnostics.turnReady,
      diagnostics.workersReady,
      diagnostics.wireGuard === 'ok',
      diagnostics.routingReady,
      diagnostics.internetReady,
    ];
    const labels = [
      'Сервер доступен',
      'TURN-соединение',
      'Воркеры запущены',
      'WireGuard настроен',
      'Маршрутизация',
      'Доступ в интернет',
    ];
    const firstPending = done.findIndex(v => !v);
    return labels.map((label, index) => ({
      label,
      state: done[index] ? 'done' : index === firstPending ? 'current' : 'pending',
    }));
  }, [diagnostics]);

  if (diagnostics.recentFailure) {
    return (
      <section className="connection-dock connection-dock--error" aria-live="polite">
        <div className="connection-dock__header">
          <div>
            <div className="connection-dock__eyebrow">{serverName}</div>
            <div className="connection-dock__title"><IconAlertTriangle size={18} />Не удалось подключиться</div>
          </div>
          <button type="button" className="dock-link" onClick={() => navigate('/logs')}>Подробнее<IconChevronRight size={16} /></button>
        </div>
        <div className="connection-error-message">{diagnostics.lastProblem?.message ?? 'Неизвестная ошибка подключения'}</div>
        {diagnostics.lastProblemHint && <div className="connection-error-hint">{diagnostics.lastProblemHint}</div>}
        <div className="connection-dock__footer">
          <span>Воркеры: {diagnostics.activeWorkers ?? '—'} / {diagnostics.totalWorkers ?? configuredWorkers}</span>
          <button type="button" className="retry-btn" onClick={onRetry}><IconRefresh size={15} />Повторить</button>
        </div>
      </section>
    );
  }

  if (tunnelState === 'connecting' || tunnelState === 'disconnecting') {
    return (
      <section className="connection-dock connection-dock--connecting" aria-live="polite">
        <div className="connection-dock__header">
          <div>
            <div className="connection-dock__eyebrow">{tunnelState === 'disconnecting' ? 'Отключение' : 'Подключение к'}</div>
            <div className="connection-dock__title">{serverName}</div>
          </div>
          <IconLoader2 className="dock-spinner" size={20} />
        </div>
        <div className="connection-steps">
          {steps.map(step => (
            <div key={step.label} className={`connection-step connection-step--${step.state}`}>
              {step.state === 'done' ? <IconCheck size={14} /> : step.state === 'current' ? <IconLoader2 size={14} /> : <IconCircle size={12} />}
              <span>{step.label}</span>
            </div>
          ))}
        </div>
      </section>
    );
  }

  return (
    <section className="connection-dock connection-dock--connected" aria-live="polite">
      <div className="connection-dock__header">
        <div>
          <div className="connection-dock__eyebrow">{serverName}</div>
          <div className="connection-dock__title"><span className="live-dot" />Подключено · {formatDuration(seconds)}</div>
        </div>
        <button type="button" className="dock-link" onClick={() => navigate('/logs')}>Подробнее<IconChevronRight size={16} /></button>
      </div>

      <div className="connection-metrics">
        <span><IconWifi size={15} />{ping != null ? `${ping} мс` : 'Ping —'}</span>
        <span>{diagnostics.activeWorkers ?? '—'} / {diagnostics.totalWorkers ?? configuredWorkers} воркеров</span>
        <span>{diagnostics.traffic ? `${diagnostics.traffic} трафика` : 'Трафик —'}</span>
      </div>

      <div className="connection-routes">
        <div><IconRoute size={15} /><span>IPv4</span><CheckBadge state={diagnostics.ipv4} okLabel="Через туннель" /></div>
        <div><IconShieldCheck size={15} /><span>IPv6</span><CheckBadge state={diagnostics.ipv6} okLabel="Защищён" /></div>
      </div>
    </section>
  );
}
