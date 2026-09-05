import { useEffect, useMemo, useState } from 'react';
import {
  IconCheck,
  IconCopy,
  IconHash,
  IconLogin,
  IconLogout,
  IconPlus,
  IconRefresh,
  IconReplace,
  IconTrash,
  IconWand,
  IconX,
} from '@tabler/icons-react';
import type { backend } from '../../wailsjs/go/models';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import {
  AddVKHash,
  CancelVKOperation,
  CancelVKHashChecks,
  CheckAllVKHashes,
  CheckVKHash,
  DeleteVKHash,
  GenerateVKHashes,
  IsVKAuthAvailable,
  IsVKLoggedIn,
  ListVKHashes,
  ReplaceVKHash,
  SyncProfileVKSettings,
  VKLogin,
  VKLogout,
} from '../../wailsjs/go/backend/App';
import { serverStore } from '../lib/store';
import { toastStore } from '../lib/stores/toastStore';
import type { Server, VKHashMode } from '../lib/types';
import { getServerVKHashPolicy } from '../lib/utils/vkHashPolicy';
import './VKHashes.css';

type Busy = 'login' | 'logout' | 'generate' | 'add' | string | null;

interface BulkProgress {
  operationId: string;
  running: boolean;
  completed: number;
  total: number;
  hashId: string;
  stage: string;
  state: string;
  elapsedMs: number;
}

function asRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value === 'string') {
    try {
      const parsed = JSON.parse(value);
      return parsed && typeof parsed === 'object' ? parsed as Record<string, unknown> : null;
    } catch {
      return null;
    }
  }
  return value && typeof value === 'object' ? value as Record<string, unknown> : null;
}

function stageLabel(stage: string): string {
  switch (stage) {
    case 'queued': return 'В очереди';
    case 'dns': return 'DNS';
    case 'credentials': return 'VK-креды';
    case 'turn': return 'TURN Allocate';
    case 'wrap': return 'WRAP';
    case 'dtls': return 'DTLS';
    case 'completed': return 'Завершение';
    default: return stage || 'Подготовка';
  }
}

function errorMessage(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  return message.replace(/^Error:\s*/i, '') || 'Неизвестная ошибка';
}

function maskedHash(hash: string): string {
  if (hash.length <= 14) return hash;
  return `${hash.slice(0, 8)}…${hash.slice(-5)}`;
}

function formatCheckedAt(value?: number): string {
  if (!value) return 'Не проверен';
  return new Date(value).toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}

function statusLabel(check?: backend.VKHashCheck): string {
  switch (check?.status) {
    case 'valid': return 'Работает';
    case 'invalid': return 'Не работает';
    case 'error': return 'Ошибка проверки';
    default: return 'Не проверен';
  }
}

function statusClass(check?: backend.VKHashCheck): string {
  switch (check?.status) {
    case 'valid': return 'valid';
    case 'invalid': return 'invalid';
    case 'error': return 'error';
    default: return 'unknown';
  }
}

async function syncServer(server: Server): Promise<void> {
  const policy = getServerVKHashPolicy(server);
  await SyncProfileVKSettings(
    server.name,
    server.host,
    server.password,
    (server.hashes ?? []).filter(Boolean),
    policy.mode,
    policy.autoCheck,
    policy.autoReplace,
  );
}

export default function VKHashes() {
  const [servers, setServers] = useState<Server[]>(() => serverStore.getAll());
  const [selectedId, setSelectedId] = useState<string>(() => {
    const all = serverStore.getAll();
    return serverStore.getLastSelectedId() ?? all[0]?.id ?? '';
  });
  const [entries, setEntries] = useState<backend.VKHashEntry[]>([]);
  const [authAvailable, setAuthAvailable] = useState(false);
  const [loggedIn, setLoggedIn] = useState(false);
  const [authChecked, setAuthChecked] = useState(false);
  const [manualHash, setManualHash] = useState('');
  const [busy, setBusy] = useState<Busy>(null);
  const [bulkProgress, setBulkProgress] = useState<BulkProgress | null>(null);

  const selected = useMemo(
    () => servers.find(server => server.id === selectedId) ?? servers[0] ?? null,
    [servers, selectedId],
  );
  const policy = selected ? getServerVKHashPolicy(selected) : null;

  const refresh = async () => {
    const list = await ListVKHashes();
    setEntries(list ?? []);
  };

  const refreshAuth = async () => {
    try {
      setLoggedIn(await IsVKLoggedIn());
    } catch {
      setLoggedIn(false);
    }
  };

  useEffect(() => {
    let active = true;
    const init = async () => {
      const current = serverStore.getAll();
      await Promise.all(current.map(server => syncServer(server).catch(() => {})));
      if (!active) return;
      setServers(serverStore.getAll());
      try {
        const available = await IsVKAuthAvailable();
        if (!active) return;
        setAuthAvailable(available);
        await refreshAuth();
      } finally {
        if (active) setAuthChecked(true);
      }
      if (active) await refresh();
    };
    void init();
    return () => { active = false; };
  }, []);

  useEffect(() => {
    const offs = [
      EventsOn('vk-hash-bulk-started', (payload: unknown) => {
        const record = asRecord(payload);
        if (!record) return;
        setBulkProgress({
          operationId: String(record.operationId ?? ''),
          running: true,
          completed: 0,
          total: Number(record.total ?? 0),
          hashId: '',
          stage: 'queued',
          state: 'running',
          elapsedMs: 0,
        });
      }),
      EventsOn('vk-hash-check-progress', (payload: unknown) => {
        const record = asRecord(payload);
        if (!record) return;
        const operationId = String(record.operationId ?? '');
        if (!operationId.startsWith('hash-bulk-')) return;
        setBulkProgress(previous => {
          if (!previous || previous.operationId !== operationId) return previous;
          return {
            ...previous,
            hashId: String(record.hashId ?? previous.hashId),
            stage: String(record.stage ?? previous.stage),
            state: String(record.state ?? previous.state),
            elapsedMs: Number(record.elapsedMs ?? previous.elapsedMs),
          };
        });
      }),
      EventsOn('vk-hash-bulk-progress', (payload: unknown) => {
        const record = asRecord(payload);
        if (!record) return;
        const operationId = String(record.operationId ?? '');
        setBulkProgress(previous => {
          if (!previous || previous.operationId !== operationId) return previous;
          return {
            ...previous,
            completed: Number(record.completed ?? previous.completed),
            total: Number(record.total ?? previous.total),
            hashId: String(record.hashId ?? previous.hashId),
            elapsedMs: Number(record.elapsedMs ?? previous.elapsedMs),
          };
        });
      }),
      EventsOn('vk-hash-check-result', () => { void refresh(); }),
      EventsOn('vk-hash-bulk-completed', (payload: unknown) => {
        const record = asRecord(payload);
        if (!record) return;
        const operationId = String(record.operationId ?? '');
        setBulkProgress(previous => {
          if (!previous || previous.operationId !== operationId) return previous;
          return {
            ...previous,
            running: false,
            completed: Number(record.completed ?? previous.completed),
            total: Number(record.total ?? previous.total),
            state: String(record.state ?? 'completed'),
            elapsedMs: Number(record.elapsedMs ?? previous.elapsedMs),
          };
        });
        void refresh();
      }),
    ];
    return () => offs.forEach(off => off());
  }, []);

  const run = async (key: Busy, action: () => Promise<void>) => {
    if (busy) return;
    setBusy(key);
    try {
      await action();
    } catch (error) {
      toastStore.show(errorMessage(error), 5000);
    } finally {
      setBusy(null);
    }
  };

  const changePolicy = async (patch: Partial<{ mode: VKHashMode; autoCheck: boolean; autoReplace: boolean }>) => {
    if (!selected || busy) return;
    const current = getServerVKHashPolicy(selected);
    const next = { ...current, ...patch };
    const updated: Server = {
      ...selected,
      hashMode: next.mode,
      hashAutoCheck: next.autoCheck,
      hashAutoReplace: next.autoReplace,
    };
    serverStore.update(updated);
    const all = serverStore.getAll();
    setServers(all);
    await run('policy', async () => {
      await syncServer(updated);
      await refresh();
    });
  };

  const login = () => run('login', async () => {
    await VKLogin();
    await refreshAuth();
    toastStore.show('VK подключён');
  });

  const logout = () => run('logout', async () => {
    await VKLogout();
    setLoggedIn(false);
    toastStore.show('VK отключён');
  });

  const addManual = () => run('add', async () => {
    if (!manualHash.trim()) return;
    await AddVKHash(manualHash, 'manual', selected?.name ?? '');
    setManualHash('');
    await refresh();
    toastStore.show('VK-хеш добавлен в общий пул');
  });

  const generate = (count: number) => run('generate', async () => {
    const existing = entries.map(entry => entry.hash);
    const generated = await GenerateVKHashes(count, existing);
    for (const hash of generated) {
      await AddVKHash(hash, 'generated', selected?.name ?? '');
    }
    await refresh();
    toastStore.show(`Создано VK-хешей: ${generated.length}`);
  });

  const checkOne = (entry: backend.VKHashEntry) => run(`check:${entry.id}`, async () => {
    if (!selected) throw new Error('Выберите сервер для функциональной проверки');
    await CheckVKHash(entry.id, selected.name);
    await refresh();
  });

  const checkAll = async () => {
    if (!selected || bulkProgress?.running) return;
    setBulkProgress({
      operationId: '', running: true, completed: 0, total: entries.length,
      hashId: '', stage: 'queued', state: 'running', elapsedMs: 0,
    });
    try {
      await CheckAllVKHashes(selected.name);
      await refresh();
      toastStore.show('Проверка VK-хешей завершена');
    } catch (error) {
      setBulkProgress(previous => previous ? { ...previous, running: false, state: 'error' } : null);
      toastStore.show(errorMessage(error), 5000);
    }
  };

  const replace = (entry: backend.VKHashEntry) => run(`replace:${entry.id}`, async () => {
    if (!selected) throw new Error('Выберите сервер для замены');
    await ReplaceVKHash(entry.id, selected.name);
    await refresh();
    const all = serverStore.getAll();
    setServers(all);
    toastStore.show('VK-хеш заменён');
  });

  const remove = (entry: backend.VKHashEntry) => run(`delete:${entry.id}`, async () => {
    await DeleteVKHash(entry.id);
    await refresh();
  });

  const copy = async (hash: string) => {
    await navigator.clipboard.writeText(hash);
    toastStore.show('VK-хеш скопирован');
  };

  const cancel = async () => {
    await Promise.allSettled([CancelVKOperation(), CancelVKHashChecks()]);
    toastStore.show('Отмена операции…');
  };

  const cancelBulk = async () => {
    await CancelVKHashChecks();
    toastStore.show('Отмена проверки VK-хешей…');
  };

  const pool = entries.filter(entry => entry.inPool);
  const bulkEntry = bulkProgress?.hashId ? entries.find(entry => entry.id === bulkProgress.hashId) : undefined;
  const local = selected
    ? entries.filter(entry => !entry.inPool && entry.usedBy?.includes(selected.name))
    : [];

  const renderEntry = (entry: backend.VKHashEntry) => {
    const check = selected ? entry.checks?.[selected.name] : undefined;
    const rowBusy = typeof busy === 'string' && busy.endsWith(entry.id);
    return (
      <div className="vkh-row" key={entry.id}>
        <div className="vkh-main">
          <div className="vkh-hash-line">
            <code>{maskedHash(entry.hash)}</code>
            <span className="vkh-source">{entry.source || 'manual'}</span>
          </div>
          <div className="vkh-meta">
            <span className={`vkh-status vkh-status--${statusClass(check)}`}>
              {check?.status === 'valid' ? <IconCheck size={14} /> : <IconHash size={14} />}
              {statusLabel(check)}
            </span>
            <span>{formatCheckedAt(check?.checkedAt)}</span>
            {check?.latencyMs ? <span>{check.latencyMs} мс</span> : null}
            {entry.usedBy?.length ? <span>Используют: {entry.usedBy.join(', ')}</span> : null}
          </div>
          {check?.message && check.status !== 'valid' ? (
            <div className="vkh-error">{check.errorType ? `${check.errorType}: ` : ''}{check.message}</div>
          ) : null}
        </div>
        <div className="vkh-row-actions">
          <button type="button" onClick={() => void copy(entry.hash)} title="Скопировать"><IconCopy size={17} /></button>
          <button type="button" onClick={() => void checkOne(entry)} disabled={Boolean(busy) || !selected} title="Проверить"><IconRefresh size={17} /></button>
          <button type="button" onClick={() => void replace(entry)} disabled={Boolean(busy) || !selected || !loggedIn} title="Заменить"><IconReplace size={17} /></button>
          {entry.inPool ? (
            <button type="button" onClick={() => void remove(entry)} disabled={Boolean(busy)} title="Убрать из общего пула"><IconTrash size={17} /></button>
          ) : null}
          {rowBusy ? <span className="vkh-spinner" /> : null}
        </div>
      </div>
    );
  };

  return (
    <main className="vkh-page">
      <div className="vkh-shell">
        <header className="vkh-header">
          <div>
            <h1>VK-хеши</h1>
            <p>Общий пул, server-aware проверка и автоматическая замена.</p>
          </div>
          {busy ? (
            <button type="button" className="vkh-btn vkh-btn--ghost" onClick={() => void cancel()}>
              <IconX size={17} /> Отменить
            </button>
          ) : null}
        </header>

        <section className="vkh-card">
          <div className="vkh-card-title">
            <div>
              <strong>Сервер и политика</strong>
              <span>Проверка выполняется относительно выбранного профиля.</span>
            </div>
          </div>
          {selected ? (
            <>
              <select
                className="vkh-select"
                value={selected.id}
                onChange={event => setSelectedId(event.target.value)}
                disabled={Boolean(busy)}
              >
                {servers.map(server => <option value={server.id} key={server.id}>{server.name} · {server.host}</option>)}
              </select>
              <div className="vkh-policy-grid">
                <label>
                  <span>Источник хешей</span>
                  <select
                    className="vkh-select"
                    value={policy?.mode ?? 'local'}
                    onChange={event => void changePolicy({ mode: event.target.value as VKHashMode })}
                    disabled={Boolean(busy)}
                  >
                    <option value="local">Только этого сервера</option>
                    <option value="local+pool">Сервер + общий пул</option>
                    <option value="pool">Только общий пул</option>
                  </select>
                </label>
                <label className="vkh-switch-row">
                  <input
                    type="checkbox"
                    checked={policy?.autoCheck ?? true}
                    onChange={event => void changePolicy({ autoCheck: event.target.checked })}
                    disabled={Boolean(busy)}
                  />
                  <span><strong>Проверять автоматически</strong><small>Перед подключением, если результат устарел.</small></span>
                </label>
                <label className="vkh-switch-row">
                  <input
                    type="checkbox"
                    checked={policy?.autoReplace ?? false}
                    onChange={event => void changePolicy({ autoReplace: event.target.checked })}
                    disabled={Boolean(busy)}
                  />
                  <span><strong>Автозамена</strong><small>Только после подтверждённого invalid hash; требуется вход в VK.</small></span>
                </label>
              </div>
            </>
          ) : (
            <div className="vkh-empty">Сначала добавьте сервер. Общим пулом можно управлять и без него, но functional check требует профиль.</div>
          )}
        </section>

        <section className="vkh-card">
          <div className="vkh-card-title">
            <div>
              <strong>VK и генерация</strong>
              <span>{!authChecked ? 'Проверка авторизации…' : authAvailable ? (loggedIn ? 'Авторизован' : 'Не авторизован') : 'Автогенерация доступна только в Windows'}</span>
            </div>
            <div className="vkh-actions">
              {authAvailable && (loggedIn ? (
                <button type="button" className="vkh-btn vkh-btn--ghost" onClick={() => void logout()} disabled={Boolean(busy)}>
                  <IconLogout size={17} /> Выйти
                </button>
              ) : (
                <button type="button" className="vkh-btn" onClick={() => void login()} disabled={Boolean(busy) || !authChecked}>
                  <IconLogin size={17} /> Войти в VK
                </button>
              ))}
              <button type="button" className="vkh-btn" onClick={() => void generate(1)} disabled={Boolean(busy) || !loggedIn}>
                <IconPlus size={17} /> Создать 1
              </button>
              <button type="button" className="vkh-btn vkh-btn--ghost" onClick={() => void generate(4)} disabled={Boolean(busy) || !loggedIn}>
                <IconWand size={17} /> Создать 4
              </button>
            </div>
          </div>
          <div className="vkh-add">
            <input
              value={manualHash}
              onChange={event => setManualHash(event.target.value)}
              placeholder="HASH или https://vk.com/call/join/HASH"
              disabled={Boolean(busy)}
            />
            <button type="button" className="vkh-btn" onClick={() => void addManual()} disabled={Boolean(busy) || !manualHash.trim()}>
              Добавить в пул
            </button>
          </div>
        </section>

        <section className="vkh-card">
          <div className="vkh-card-title">
            <div>
              <strong>Общий пул · {pool.length}</strong>
              <span>Один экземпляр хеша может использоваться несколькими серверами.</span>
            </div>
            <button type="button" className="vkh-btn vkh-btn--ghost" onClick={() => void checkAll()} disabled={Boolean(busy) || bulkProgress?.running || !selected || entries.length === 0}>
              <IconRefresh size={17} /> {bulkProgress?.running ? 'Проверяется…' : 'Проверить все'}
            </button>
          </div>
          {bulkProgress ? (
            <div className={`vkh-progress vkh-progress--${bulkProgress.state}`}>
              <div className="vkh-progress-head">
                <div>
                  <strong>{bulkProgress.running ? 'Проверка VK-хешей' : bulkProgress.state === 'canceled' ? 'Проверка отменена' : 'Проверка завершена'}</strong>
                  <span>{bulkProgress.completed} / {bulkProgress.total}</span>
                </div>
                {bulkProgress.running ? (
                  <button type="button" className="vkh-btn vkh-btn--ghost" onClick={() => void cancelBulk()}>
                    <IconX size={16} /> Отменить
                  </button>
                ) : null}
              </div>
              <div className="vkh-progress-body">
                <code>{bulkEntry ? maskedHash(bulkEntry.hash) : '—'}</code>
                <span>{stageLabel(bulkProgress.stage)}</span>
                <span>{(bulkProgress.elapsedMs / 1000).toFixed(1)} с</span>
              </div>
              <div className="vkh-progress-track">
                <span style={{ width: `${bulkProgress.total ? Math.min(100, (bulkProgress.completed / bulkProgress.total) * 100) : 0}%` }} />
              </div>
            </div>
          ) : null}
          <div className="vkh-list">
            {pool.length ? pool.map(renderEntry) : <div className="vkh-empty">Общий пул пока пуст.</div>}
          </div>
        </section>

        {selected ? (
          <section className="vkh-card">
            <div className="vkh-card-title">
              <div>
                <strong>Локальные хеши · {selected.name}</strong>
                <span>Эти значения принадлежат только профилю и не попадают в общий пул автоматически.</span>
              </div>
            </div>
            <div className="vkh-list">
              {local.length ? local.map(renderEntry) : <div className="vkh-empty">Отдельных локальных хешей нет.</div>}
            </div>
          </section>
        ) : null}
      </div>
    </main>
  );
}
