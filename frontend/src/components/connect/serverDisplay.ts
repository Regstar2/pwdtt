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
