import type { Server } from '../../lib/types';

const FLAG_CODES = ['ru', 'us', 'de', 'nl', 'fi', 'fr', 'gb', 'jp', 'pl', 'se', 'ch', 'lt', 'lv', 'ee', 'cz', 'at', 'ca', 'au', 'sg', 'hk', 'tr', 'kz'];

export const SERVER_ICON_KEYS = [
  'clover', 'flame', 'shield', 'grid', 'cloud', 'speed', 'star', 'heart',
  'bolt', 'rocket', 'crown', 'diamond', 'leaf', 'snowflake', 'server',
  'globe', 'lock', 'wifi',
  ...FLAG_CODES.map(code => `flag-${code}`),
];

export function isServerFlagCode(code: string) {
  return FLAG_CODES.includes(code);
}

export function pingColor(ping?: number) {
  if (ping == null) return 'var(--text-4)';
  if (ping < 100) return '#22c55e';
  if (ping < 200) return '#f59e0b';
  return '#ef4444';
}

export function getServerHashCount(server: Server) {
  return (server.hashes ?? []).filter(hash => hash.trim()).length;
}

export function formatHashCount(count: number) {
  const mod10 = count % 10;
  const mod100 = count % 100;
  if (mod10 === 1 && mod100 !== 11) return `${count} хеш`;
  if (mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14)) return `${count} хеша`;
  return `${count} хешей`;
}

export function getServerWorkers(server: Server) {
  const hashes = getServerHashCount(server);
  let workers = server.power || Math.max(9, hashes * 9);
  workers = Math.max(9, Math.min(108, workers));
  return Math.floor(workers / 9) * 9;
}
