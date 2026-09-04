import type { Server, VKHashMode } from '../types';

export interface ServerVKHashPolicy {
  mode: VKHashMode;
  autoCheck: boolean;
  autoReplace: boolean;
}

export function getServerVKHashPolicy(server: Pick<Server, 'hashMode' | 'hashAutoCheck' | 'hashAutoReplace'>): ServerVKHashPolicy {
  return {
    mode: server.hashMode ?? 'local',
    autoCheck: server.hashAutoCheck ?? true,
    autoReplace: server.hashAutoReplace ?? false,
  };
}

export function hashModeUsesLocal(mode: VKHashMode): boolean {
  return mode !== 'pool';
}

export function hashModeUsesPool(mode: VKHashMode): boolean {
  return mode !== 'local';
}
