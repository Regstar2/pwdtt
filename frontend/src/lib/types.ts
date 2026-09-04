export type VKHashMode = 'local' | 'local+pool' | 'pool';

export interface Server {
  id: string;
  name: string;
  host: string;
  password: string;
  deviceId?: string;
  ping?: number;
  icon?: string;
  hashes?: [string, string, string, string];
  power?: number;
  hashMode?: VKHashMode;
  hashAutoCheck?: boolean;
  hashAutoReplace?: boolean;
}

export interface AppSettings {
  autoStart: boolean;
  obfsMode: 'audio' | 'video';
  obfsAccepted: boolean;
}

export type TunnelState = 'idle' | 'connecting' | 'connected' | 'disconnecting';

export const DEFAULT_SETTINGS: AppSettings = {
  autoStart: true,
  obfsMode: 'audio',
  obfsAccepted: false,
};
