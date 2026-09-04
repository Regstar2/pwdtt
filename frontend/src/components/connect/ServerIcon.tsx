import type { ReactNode } from 'react';
import {
  IconCloverFilled, IconFlameFilled, IconShieldFilled, IconLayoutGridFilled,
  IconCloudFilled, IconBrandSpeedtest, IconStarFilled, IconHeartFilled,
  IconBoltFilled, IconRocket, IconCrownFilled, IconDiamondFilled,
  IconLeafFilled, IconSnowflake, IconServer, IconGlobe, IconLockFilled, IconWifi,
} from '@tabler/icons-react';

export const SERVER_ICONS: { key: string; render: (size: number) => ReactNode }[] = [
  { key: 'clover', render: s => <IconCloverFilled size={s} /> },
  { key: 'flame', render: s => <IconFlameFilled size={s} /> },
  { key: 'shield', render: s => <IconShieldFilled size={s} /> },
  { key: 'grid', render: s => <IconLayoutGridFilled size={s} /> },
  { key: 'cloud', render: s => <IconCloudFilled size={s} /> },
  { key: 'speed', render: s => <IconBrandSpeedtest size={s} stroke={2} /> },
  { key: 'star', render: s => <IconStarFilled size={s} /> },
  { key: 'heart', render: s => <IconHeartFilled size={s} /> },
  { key: 'bolt', render: s => <IconBoltFilled size={s} /> },
  { key: 'rocket', render: s => <IconRocket size={s} stroke={2} /> },
  { key: 'crown', render: s => <IconCrownFilled size={s} /> },
  { key: 'diamond', render: s => <IconDiamondFilled size={s} /> },
  { key: 'leaf', render: s => <IconLeafFilled size={s} /> },
  { key: 'snowflake', render: s => <IconSnowflake size={s} stroke={2} /> },
  { key: 'server', render: s => <IconServer size={s} stroke={2} /> },
  { key: 'globe', render: s => <IconGlobe size={s} stroke={2} /> },
  { key: 'lock', render: s => <IconLockFilled size={s} /> },
  { key: 'wifi', render: s => <IconWifi size={s} stroke={2} /> },
  ...['ru','us','de','nl','fi','fr','gb','jp','pl','se','ch','lt','lv','ee','cz','at','ca','au','sg','hk','tr','kz'].map(code => ({
    key: `flag-${code}`,
    render: (s: number) => <img src={`/flags/${code}.svg`} alt="" width={s} height={s * 0.67} style={{ objectFit: 'cover', borderRadius: 3 }} />,
  })),
];

export function ServerIcon({ iconKey, size }: { iconKey?: string; size: number }) {
  const entry = SERVER_ICONS.find(item => item.key === (iconKey ?? 'clover')) ?? SERVER_ICONS[0];
  return <>{entry.render(size)}</>;
}

export function pingColor(ping?: number) {
  if (ping == null) return 'var(--text-4)';
  if (ping < 100) return '#22c55e';
  if (ping < 200) return '#f59e0b';
  return '#ef4444';
}
