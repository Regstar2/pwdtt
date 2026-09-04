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

function groupKey(message: string): string {
  let normalized = message
    .replace(/^\[(ВОРКЕР|WORKER)\s*#?\d+\]/i, '[$1]')
    .replace(/^\[(ВОРКЕР|WORKER)\s+\d+\]/i, '[$1]');

  if (/^\[(STATS|СТАТ)/i.test(normalized)) {
    normalized = normalized
      .replace(/Активных\s*:\s*\d+/i, 'Активных:*')
      .replace(/Трафик\s*:\s*[\d.,]+\s*(?:Б|КБ|МБ|ГБ|ТБ|B|KB|MB|GB|TB)/i, 'Трафик:*');
  }

  return normalized;
}

export const logStore = {
  subscribe: (fn: Listener) => {
    listeners.add(fn);
    fn([...entries]);
    return () => { listeners.delete(fn); };
  },

  push: (level: LogLevel, message: string) => {
    const time = new Date().toLocaleTimeString('ru-RU', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    const key = groupKey(message);
    const idx = entries.findLastIndex(e => e.level === level && groupKey(e.message) === key);

    if (idx !== -1) {
      const found = entries[idx];
      entries = [
        ...entries.slice(0, idx),
        { ...found, message, time, count: found.count + 1 },
        ...entries.slice(idx + 1),
      ];
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
