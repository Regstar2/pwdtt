import type { TunnelState } from './types';
import type { LogEntry } from './stores/logStore';

export type LogSubsystem = 'Core' | 'TURN' | 'Worker' | 'WireGuard' | 'Routing' | 'DNS' | 'Health' | 'Other';
export type CheckState = 'ok' | 'error' | 'unknown';

export interface ConnectionDiagnostics {
  activeWorkers: number | null;
  totalWorkers: number | null;
  traffic: string | null;
  wireGuard: CheckState;
  ipv4: CheckState;
  ipv6: CheckState;
  dns: CheckState;
  lastProblem: LogEntry | null;
  lastProblemHint: string | null;
  recentFailure: boolean;
  serverReachable: boolean;
  turnReady: boolean;
  workersReady: boolean;
  routingReady: boolean;
  internetReady: boolean;
}

const positive = /(ok|готов|актив|успеш|установ|connected|running|примен|через\s+туннель|through\s+tunnel|защищ|заблокирован|blocked)/i;
const negative = /(error|ошиб|fail|утеч|leak)/i;

export function getLogSubsystem(message: string): LogSubsystem {
  const tag = message.match(/^\[([^\]]+)\]/)?.[1]?.toUpperCase() ?? '';
  if (tag.includes('TURN')) return 'TURN';
  if (tag.includes('WORKER') || tag.includes('ВОРКЕР')) return 'Worker';
  if (tag === 'WG' || tag.includes('WIREGUARD')) return 'WireGuard';
  if (tag.includes('ROUT') || tag.includes('МАРШРУТ')) return 'Routing';
  if (tag.includes('DNS')) return 'DNS';
  if (tag.includes('HEALTH') || tag.includes('STATS') || tag.includes('СТАТ')) return 'Health';
  if (tag.includes('CORE') || tag.includes('ЯДРО')) return 'Core';

  const upper = message.toUpperCase();
  if (upper.includes('WIREGUARD') || upper.includes('[WG]')) return 'WireGuard';
  if (upper.includes('IPV4') || upper.includes('IPV6') || upper.includes('ROUT')) return 'Routing';
  if (upper.includes('DNS')) return 'DNS';
  if (upper.includes('WORKER') || upper.includes('ВОРКЕР')) return 'Worker';
  if (upper.includes('TURN')) return 'TURN';
  return 'Other';
}

function latestMatch(entries: LogEntry[], matcher: (entry: LogEntry) => boolean): LogEntry | null {
  for (let i = entries.length - 1; i >= 0; i -= 1) {
    if (matcher(entries[i])) return entries[i];
  }
  return null;
}

function parseWorkers(entries: LogEntry[]) {
  for (let i = entries.length - 1; i >= 0; i -= 1) {
    const message = entries[i].message;
    const pair = message.match(/(?:Воркеры|Workers?)\s*:\s*(\d+)\s*\/\s*(\d+)/i);
    if (pair) return { active: Number(pair[1]), total: Number(pair[2]) };

    const active = message.match(/Активных\s*:\s*(\d+)/i);
    if (active) return { active: Number(active[1]), total: null };
  }
  return { active: null, total: null };
}

function parseTraffic(entries: LogEntry[]): string | null {
  for (let i = entries.length - 1; i >= 0; i -= 1) {
    const m = entries[i].message.match(/Трафик\s*:\s*([\d.,]+\s*(?:Б|КБ|МБ|ГБ|ТБ|B|KB|MB|GB|TB))/i);
    if (m) return m[1].replace(',', '.');
  }
  return null;
}

function checkFromLogs(entries: LogEntry[], matcher: RegExp): CheckState {
  const entry = latestMatch(entries, e => matcher.test(e.message));
  if (!entry) return 'unknown';
  if (entry.level === 'ERROR' || negative.test(entry.message)) return 'error';
  return positive.test(entry.message) ? 'ok' : 'unknown';
}

export function explainKnownProblem(message: string): string | null {
  if (/reader\s*:\s*eof/i.test(message)) {
    return 'Соединение воркера закрыто удалённой стороной. Если часть воркеров остаётся активной, туннель может продолжать работу.';
  }
  if (/timeout|timed out|таймаут/i.test(message)) {
    return 'Удалённая сторона не ответила вовремя. Проверьте доступность сервера и повторите подключение.';
  }
  if (/dns/i.test(message) && negative.test(message)) {
    return 'Не удалось разрешить доменное имя. Проверьте DNS и состояние туннеля.';
  }
  return null;
}

export function buildConnectionDiagnostics(entries: LogEntry[], tunnelState: TunnelState): ConnectionDiagnostics {
  const workers = parseWorkers(entries);
  const lastProblem = latestMatch(entries, e => e.level === 'ERROR' || e.level === 'WARN');
  const lastDisconnectIndex = entries.findLastIndex(e => /—\s*Отключено|disconnected|stopped/i.test(e.message));
  const lastErrorIndex = entries.findLastIndex(e => e.level === 'ERROR');

  const wireGuardFromLogs = checkFromLogs(entries, /\[WG\]|WireGuard/i);
  const ipv4 = checkFromLogs(entries, /IPv4/i);
  let ipv6: CheckState = checkFromLogs(entries, /IPv6/i);
  const ipv6Blocked = latestMatch(entries, e =>
    /IPv6/i.test(e.message) && /(заблокирован|blocked|leak protection\s*:\s*ok|защита.*ok)/i.test(e.message)
  );
  if (ipv6Blocked) ipv6 = 'ok';

  const dns = checkFromLogs(entries, /\bDNS\b/i);
  const hasTurn = entries.some(e => getLogSubsystem(e.message) === 'TURN' && positive.test(e.message));
  const hasWorker = workers.active !== null
    ? workers.active > 0
    : entries.some(e => getLogSubsystem(e.message) === 'Worker' && positive.test(e.message));
  const serverReachable = entries.some(e =>
    /(сервер.*доступ|server.*reachable|peer.*connected|соединение.*установ)/i.test(e.message)
  ) || hasTurn;

  const routingReady = ipv4 === 'ok' || entries.some(e =>
    getLogSubsystem(e.message) === 'Routing' && positive.test(e.message) && !/IPv6/i.test(e.message)
  );
  const internetReady = entries.some(e =>
    /(доступ.*интернет|internet.*(ok|available|reachable)|health.*ok)/i.test(e.message)
  );

  return {
    activeWorkers: workers.active,
    totalWorkers: workers.total,
    traffic: parseTraffic(entries),
    wireGuard: tunnelState === 'connected' ? 'ok' : wireGuardFromLogs,
    ipv4,
    ipv6,
    dns,
    lastProblem,
    lastProblemHint: lastProblem ? explainKnownProblem(lastProblem.message) : null,
    recentFailure: tunnelState === 'idle' && lastErrorIndex > lastDisconnectIndex,
    serverReachable,
    turnReady: hasTurn,
    workersReady: hasWorker,
    routingReady,
    internetReady,
  };
}
