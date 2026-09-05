export type LogLevel = 'INFO' | 'ERROR' | 'WARN' | 'DEBUG';

export interface LogEntry {
  id: number;
  level: LogLevel;
  message: string;
  time: string;
  count: number;
}

type Listener = (entries: LogEntry[]) => void;

let seq = 0;
let entries: LogEntry[] = [];
const listeners = new Set<Listener>();
const MAX_ENTRIES = 500;

function notify() {
  listeners.forEach(fn => fn([...entries]));
}

function extractTag(message: string): string {
  const m = message.match(/^\[([^\]]+)\]/);
  return m?.[1] ?? '';
}

function groupingKey(message: string): string {
  // STATS is a live snapshot: keep only the latest sample while counting updates.
  if (extractTag(message) === 'STATS') return '[STATS]';
  // Other INFO/WARN entries are grouped only when the actual message repeats.
  // Different events from the same subsystem (for example [WG]) must stay distinct.
  return message;
}

export const logStore = {
  subscribe: (fn: Listener) => {
    listeners.add(fn);
    fn([...entries]);
    return () => { listeners.delete(fn); };
  },

  push: (level: LogLevel, message: string) => {
    const time = new Date().toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' });

    if (level === 'ERROR' || level === 'DEBUG') {
      entries = [...entries, { id: seq++, level, message, time, count: 1 }];
      if (entries.length > MAX_ENTRIES) entries = entries.slice(-MAX_ENTRIES);
      notify();
      return;
    }

    const key = groupingKey(message);
    const idx = entries.findIndex(e => e.level === level && groupingKey(e.message) === key);
    if (idx !== -1) {
      const found = entries[idx];
      const updated = { ...found, message, time, count: found.count + 1 };
      // The grouped row represents the latest occurrence/snapshot, so keep it
      // at the end of the timeline instead of changing a timestamp in-place.
      entries = [...entries.slice(0, idx), ...entries.slice(idx + 1), updated];
      notify();
      return;
    }

    entries = [...entries, { id: seq++, level, message, time, count: 1 }];
    if (entries.length > MAX_ENTRIES) entries = entries.slice(-MAX_ENTRIES);
    notify();
  },

  clear: () => {
    entries = [];
    notify();
  },

  getAll: () => [...entries],
};
