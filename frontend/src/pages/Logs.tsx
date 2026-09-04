import { useState, useEffect, useRef, useCallback, useMemo } from 'react';
import { IconSearch, IconTrashX, IconCopy, IconCheck, IconAlertTriangle } from '@tabler/icons-react';
import { logStore, type LogEntry, type LogLevel } from '../lib/stores/logStore';
import { tunnelStore } from '../lib/stores/tunnelStore';
import { serverStore } from '../lib/store';
import {
  buildConnectionDiagnostics,
  getLogSubsystem,
  type CheckState,
  type LogSubsystem,
} from '../lib/connectionDiagnostics';
import './Logs.css';

type Filter = 'ALL' | LogLevel;
type SubsystemFilter = 'ALL' | LogSubsystem;

const LEVEL_COLOR: Record<LogLevel, string> = {
  INFO:  'var(--text)',
  WARN:  '#f59e0b',
  ERROR: '#ef4444',
  DEBUG: 'var(--text-3)',
};

const LEVELS: Filter[] = ['ALL', 'INFO', 'WARN', 'ERROR', 'DEBUG'];
const SUBSYSTEMS: SubsystemFilter[] = ['ALL', 'Core', 'TURN', 'Worker', 'WireGuard', 'Routing', 'DNS', 'Health', 'Other'];

function checkText(state: CheckState, ok: string) {
  if (state === 'ok') return ok;
  if (state === 'error') return 'Проблема';
  return 'Нет данных';
}

function connectionText(state: ReturnType<typeof tunnelStore.get>) {
  if (state === 'connected') return 'ПОДКЛЮЧЕНО';
  if (state === 'connecting') return 'ПОДКЛЮЧЕНИЕ';
  if (state === 'disconnecting') return 'ОТКЛЮЧЕНИЕ';
  return 'ОТКЛЮЧЕНО';
}

export default function Logs() {
  const [filter, setFilter] = useState<Filter>('ALL');
  const [subsystem, setSubsystem] = useState<SubsystemFilter>('ALL');
  const [search, setSearch] = useState('');
  const [entries, setEntries] = useState<LogEntry[]>([]);
  const [tunnelState, setTunnelState] = useState(() => tunnelStore.get());
  const [copied, setCopied] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const autoScroll = useRef(true);

  useEffect(() => logStore.subscribe(setEntries), []);
  useEffect(() => tunnelStore.subscribe(setTunnelState), []);

  useEffect(() => {
    if (autoScroll.current) bottomRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [entries]);

  const onScroll = useCallback(() => {
    const el = listRef.current;
    if (!el) return;
    autoScroll.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  }, []);

  const diagnostics = useMemo(
    () => buildConnectionDiagnostics(entries, tunnelState),
    [entries, tunnelState],
  );

  const selectedServer = useMemo(() => {
    const all = serverStore.getAll();
    const id = serverStore.getLastSelectedId();
    return all.find(s => s.id === id) ?? all[0] ?? null;
  }, []);

  const visible = useMemo(() => entries.filter(e => {
    if (filter !== 'ALL' && e.level !== filter) return false;
    if (subsystem !== 'ALL' && getLogSubsystem(e.message) !== subsystem) return false;
    if (search && !e.message.toLowerCase().includes(search.toLowerCase())) return false;
    return true;
  }), [entries, filter, subsystem, search]);

  const copiedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const handleCopy = () => {
    const summary = [
      `Состояние: ${connectionText(tunnelState)}`,
      `Сервер: ${selectedServer?.name ?? 'не выбран'}`,
      `Воркеры: ${diagnostics.activeWorkers ?? '—'} / ${diagnostics.totalWorkers ?? selectedServer?.power ?? '—'}`,
      `WireGuard: ${checkText(diagnostics.wireGuard, 'активен')}`,
      `Маршрут IPv4: ${checkText(diagnostics.ipv4, 'OK')}`,
      `IPv6 leak protection: ${checkText(diagnostics.ipv6, 'OK')}`,
      diagnostics.lastProblem ? `Последняя проблема: ${diagnostics.lastProblem.message}` : 'Последняя проблема: нет',
      '',
      'Логи:',
    ];
    const rows = visible.map(e =>
      `[${e.time}] [${e.level}] [${getLogSubsystem(e.message)}] ${e.message}${e.count > 1 ? ` (×${e.count})` : ''}`
    );
    navigator.clipboard.writeText([...summary, ...rows].join('\n'));
    setCopied(true);
    if (copiedTimerRef.current) clearTimeout(copiedTimerRef.current);
    copiedTimerRef.current = setTimeout(() => setCopied(false), 1500);
  };

  useEffect(() => {
    return () => { if (copiedTimerRef.current) clearTimeout(copiedTimerRef.current); };
  }, []);

  return (
    <main className="logs-main">
      <div className="logs-card">
        <section className="diagnostics-summary">
          <div className="diagnostics-summary__top">
            <div>
              <span className="diagnostics-summary__label">Состояние</span>
              <strong className={`diagnostics-summary__state diagnostics-summary__state--${tunnelState}`}>
                {connectionText(tunnelState)}
              </strong>
            </div>
            <div>
              <span className="diagnostics-summary__label">Сервер</span>
              <strong>{selectedServer?.name ?? 'Не выбран'}</strong>
            </div>
            <div>
              <span className="diagnostics-summary__label">Воркеры</span>
              <strong>{diagnostics.activeWorkers ?? '—'} / {diagnostics.totalWorkers ?? selectedServer?.power ?? '—'}</strong>
            </div>
            <div>
              <span className="diagnostics-summary__label">WireGuard</span>
              <strong>{checkText(diagnostics.wireGuard, 'Активен')}</strong>
            </div>
            <div>
              <span className="diagnostics-summary__label">IPv4</span>
              <strong>{checkText(diagnostics.ipv4, 'Через туннель')}</strong>
            </div>
            <div>
              <span className="diagnostics-summary__label">IPv6</span>
              <strong>{checkText(diagnostics.ipv6, 'Защищён')}</strong>
            </div>
          </div>
          {diagnostics.lastProblem && (
            <div className="diagnostics-problem">
              <IconAlertTriangle size={15} />
              <div>
                <span>{diagnostics.lastProblem.message}</span>
                {diagnostics.lastProblemHint && <small>{diagnostics.lastProblemHint}</small>}
              </div>
              {diagnostics.lastProblem.count > 1 && <b>×{diagnostics.lastProblem.count}</b>}
            </div>
          )}
        </section>

        <div className="logs-toolbar">
          <div className="search-wrap">
            <div className="search-inner">
              <input
                className="search-input"
                placeholder="Поиск..."
                value={search}
                onChange={e => setSearch(e.target.value)}
              />
              <IconSearch size={18} className="search-icon" />
            </div>
          </div>
          <div className="logs-toolbar-right">
            <select
              className="subsystem-select"
              value={subsystem}
              onChange={e => setSubsystem(e.target.value as SubsystemFilter)}
              aria-label="Подсистема"
            >
              {SUBSYSTEMS.map(s => <option key={s} value={s}>{s === 'ALL' ? 'Все подсистемы' : s}</option>)}
            </select>
            <div className="filter-group">
              {LEVELS.map(f => (
                <button
                  type="button"
                  key={f}
                  className={`filter-btn${filter === f ? ' filter-btn--active' : ''}`}
                  onClick={() => setFilter(f)}
                >
                  {f}
                </button>
              ))}
            </div>
            <button type="button" className="icon-btn" onClick={logStore.clear} title="Очистить" aria-label="Очистить логи">
              <IconTrashX stroke={2} size={16} />
            </button>
            <button type="button" className={`icon-btn${copied ? ' icon-btn--copied' : ''}`} onClick={handleCopy} title={copied ? 'Скопировано!' : 'Копировать диагностику'} aria-label="Копировать диагностику">
              {copied ? <IconCheck stroke={2} size={16} /> : <IconCopy stroke={2} size={16} />}
            </button>
          </div>
        </div>

        {visible.length === 0 ? (
          <div className="logs-empty">{entries.length === 0 ? 'Логи появятся здесь...' : 'Ничего не найдено'}</div>
        ) : (
          <div className="logs-list" ref={listRef} onScroll={onScroll}>
            {visible.map(e => (
              <div key={e.id} className="log-row">
                <span className="log-time">{e.time}</span>
                <span className="log-level" style={{ color: LEVEL_COLOR[e.level] }}>{e.level}</span>
                <span className="log-subsystem">{getLogSubsystem(e.message)}</span>
                <span className="log-msg">{e.message}</span>
                {e.count > 1 && <span className="log-count">×{e.count}</span>}
              </div>
            ))}
            <div ref={bottomRef} />
          </div>
        )}
      </div>
    </main>
  );
}
