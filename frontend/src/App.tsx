import { useEffect, useState, useRef } from 'react';
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import Layout from './components/Layout';
import Connect from './pages/Connect';
import Logs from './pages/Logs';
import VKHashes from './pages/VKHashes';
import Toast from './components/Toast';
import UpdateModal from './modals/UpdateModal';
import { wdttLinkStore, parseWdttUrl } from './lib/utils/wdttLink';
import { toastStore } from './lib/stores/toastStore';
import { logStore } from './lib/stores/logStore';
import { tunnelStore } from './lib/stores/tunnelStore';
import { connectionStore } from './lib/stores/connectionStore';
import type { ConnectionProgress, ConnectionStage, ConnectionStageState } from './lib/stores/connectionStore';
import type { LogLevel } from './lib/stores/logStore';
import { EventsOn } from '../wailsjs/runtime/runtime';
import { CheckUpdate } from '../wailsjs/go/backend/App';

function useWdttPaste() {
  useEffect(() => {
    const handler = (e: ClipboardEvent) => {
      const text = e.clipboardData?.getData('text') ?? '';
      const trimmed = text.trim();
      if (!trimmed.startsWith('qwdtt://') && !trimmed.startsWith('wdtt://')) return;
      const tag = (document.activeElement as HTMLElement)?.tagName;
      if (tag === 'INPUT' || tag === 'TEXTAREA') return;
      e.preventDefault();
      const link = parseWdttUrl(text.trim());
      if (!link) { toastStore.show('Неверный формат ссылки'); return; }
      wdttLinkStore.set(link);
    };
    document.addEventListener('paste', handler);
    document.body.tabIndex = 0;
    return () => document.removeEventListener('paste', handler);
  }, []);
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

function parseProgress(value: unknown): ConnectionProgress | null {
  const record = asRecord(value);
  if (!record) return null;
  const stage = String(record.stage ?? '') as ConnectionStage;
  const state = String(record.state ?? '') as ConnectionStageState;
  if (!['dns', 'vk', 'turn', 'wrap', 'dtls', 'workers', 'vpn'].includes(stage)) return null;
  if (!['pending', 'running', 'success', 'warning', 'error'].includes(state)) return null;
  return { stage, state, message: String(record.message ?? '') };
}

function useWailsEvents() {
  useEffect(() => {
    const MIN_PROGRESS_VISIBLE_MS = 260;
    const presentationQueue: Array<{ key: string; apply: () => void }> = [];
    const seenPresentationKeys = new Set<string>();
    let presentationTimer: ReturnType<typeof setTimeout> | null = null;

    const clearPresentationQueue = () => {
      presentationQueue.length = 0;
      seenPresentationKeys.clear();
      if (presentationTimer) {
        clearTimeout(presentationTimer);
        presentationTimer = null;
      }
    };

    const drainPresentationQueue = () => {
      if (presentationTimer || presentationQueue.length === 0) return;
      const item = presentationQueue.shift();
      if (!item) return;
      item.apply();
      presentationTimer = setTimeout(() => {
        presentationTimer = null;
        drainPresentationQueue();
      }, MIN_PROGRESS_VISIBLE_MS);
    };

    const enqueuePresentation = (key: string, apply: () => void) => {
      if (seenPresentationKeys.has(key)) return;
      seenPresentationKeys.add(key);
      presentationQueue.push({ key, apply });
      drainPresentationQueue();
    };

    const offs = [
      EventsOn('log', (level: unknown, msg: unknown) => {
        logStore.push((level as LogLevel) ?? 'INFO', String(msg ?? ''));
      }),
      EventsOn('diagnostic_event', (payload: unknown) => {
        const record = asRecord(payload);
        if (!record) return;
        const rawLevel = String(record.level ?? 'DEBUG').toUpperCase();
        const level: LogLevel = ['INFO', 'WARN', 'ERROR', 'DEBUG'].includes(rawLevel)
          ? rawLevel as LogLevel
          : 'DEBUG';
        const parts = [
          record.subsystem ? `[${String(record.subsystem)}]` : '[DIAG]',
          record.operationId ? `op=${String(record.operationId)}` : '',
          record.workerId ? `worker=${String(record.workerId)}` : '',
          record.hashId ? `hash=${String(record.hashId)}` : '',
          record.server ? `server=${String(record.server)}` : '',
          record.stage ? `stage=${String(record.stage)}` : '',
          record.action ? `action=${String(record.action)}` : '',
          record.result ? `result=${String(record.result)}` : '',
          record.attempt ? `attempt=${String(record.attempt)}` : '',
          record.durationMs ? `duration=${String(record.durationMs)}ms` : '',
          record.message ? String(record.message) : '',
        ].filter(Boolean);
        logStore.push(level, parts.join(' '));
      }),
      EventsOn('error', (msg: unknown) => {
        const s = String(msg ?? '');
        connectionStore.setError(s);
        logStore.push('ERROR', s);
        toastStore.show(s, 5000);
      }),
      EventsOn('stats', (payload: unknown) => {
        const record = asRecord(payload);
        if (!record) return;
        connectionStore.stats(
          Number(record.active ?? 0),
          Number(record.bytes_up ?? 0),
          Number(record.bytes_down ?? 0),
        );
      }),
      EventsOn('connection_progress', (payload: unknown) => {
        const progress = parseProgress(payload);
        if (!progress) return;

        if (progress.state === 'warning' || progress.state === 'error') {
          connectionStore.progress(progress);
          return;
        }

        enqueuePresentation(
          `${progress.stage}:${progress.state}`,
          () => connectionStore.progress(progress),
        );
      }),
      EventsOn('state_changed', (status: unknown) => {
        const s = String(status ?? '');
        if (s === 'connected' || s === 'running') {
          tunnelStore.set('connected');
          enqueuePresentation('state:connected', () => {
            connectionStore.connected();
            logStore.push('INFO', '✓ Туннель активен');
          });
        } else if (s === 'connecting') {
          tunnelStore.set('connecting');
          connectionStore.setTunnelState('connecting');
          logStore.clear();
          logStore.push('INFO', '⟳ Подключение...');
        } else if (s === 'stopped' || s === 'error' || s === 'disconnected') {
          clearPresentationQueue();
          tunnelStore.set('idle');
          connectionStore.disconnected();
          logStore.push('INFO', '— Отключено');
        }
      }),
    ];
    return () => {
      clearPresentationQueue();
      offs.forEach(off => off());
    };
  }, []);
}

const RETRY_MS = 5 * 60 * 1000;

function useUpdateChecker() {
  const [updateInfo, setUpdateInfo] = useState<{ version: string; body: string; url: string } | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    const check = () => {
      CheckUpdate().then(info => {
        if (info.available) {
          setUpdateInfo({ version: info.version, body: info.body, url: info.url });
        }
      }).catch(() => {
        timerRef.current = setTimeout(check, RETRY_MS);
      });
    };
    check();
    return () => { if (timerRef.current) clearTimeout(timerRef.current); };
  }, []);

  return { updateInfo, closeUpdate: () => setUpdateInfo(null) };
}

export default function App() {
  useWailsEvents();
  useWdttPaste();
  const { updateInfo, closeUpdate } = useUpdateChecker();

  return (
    <BrowserRouter>
      <Routes>
        <Route element={<Layout />}>
          <Route path="/" element={<Connect />} />
          <Route path="/logs" element={<Logs />} />
          <Route path="/hashes" element={<VKHashes />} />
        </Route>
      </Routes>
      <Toast />
      {updateInfo && (
        <UpdateModal
          version={updateInfo.version}
          body={updateInfo.body}
          url={updateInfo.url}
          onClose={closeUpdate}
        />
      )}
    </BrowserRouter>
  );
}
