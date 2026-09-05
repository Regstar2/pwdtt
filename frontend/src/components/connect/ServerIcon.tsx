import type { ReactNode } from 'react';
import {
  IconCloverFilled, IconFlameFilled, IconShieldFilled, IconLayoutGridFilled,
  IconCloudFilled, IconBrandSpeedtest, IconStarFilled, IconHeartFilled,
  IconBoltFilled, IconRocket, IconCrownFilled, IconDiamondFilled,
  IconLeafFilled, IconSnowflake, IconServer, IconGlobe, IconLockFilled, IconWifi,
} from '@tabler/icons-react';
import { isServerFlagCode } from './serverDisplay';

const ICON_RENDERERS: Record<string, (size: number) => ReactNode> = {
  clover: s => <IconCloverFilled size={s} />,
  flame: s => <IconFlameFilled size={s} />,
  shield: s => <IconShieldFilled size={s} />,
  grid: s => <IconLayoutGridFilled size={s} />,
  cloud: s => <IconCloudFilled size={s} />,
  speed: s => <IconBrandSpeedtest size={s} stroke={2} />,
  star: s => <IconStarFilled size={s} />,
  heart: s => <IconHeartFilled size={s} />,
  bolt: s => <IconBoltFilled size={s} />,
  rocket: s => <IconRocket size={s} stroke={2} />,
  crown: s => <IconCrownFilled size={s} />,
  diamond: s => <IconDiamondFilled size={s} />,
  leaf: s => <IconLeafFilled size={s} />,
  snowflake: s => <IconSnowflake size={s} stroke={2} />,
  server: s => <IconServer size={s} stroke={2} />,
  globe: s => <IconGlobe size={s} stroke={2} />,
  lock: s => <IconLockFilled size={s} />,
  wifi: s => <IconWifi size={s} stroke={2} />,
};

export function ServerIcon({ iconKey, size }: { iconKey?: string; size: number }) {
  const key = iconKey ?? 'clover';
  if (key.startsWith('flag-')) {
    const code = key.slice('flag-'.length);
    if (isServerFlagCode(code)) {
      return <img src={`/flags/${code}.svg`} alt="" width={size} height={size * 0.67} style={{ objectFit: 'cover', borderRadius: 3 }} />;
    }
  }

  const render = ICON_RENDERERS[key] ?? ICON_RENDERERS.clover;
  return <>{render(size)}</>;
}
