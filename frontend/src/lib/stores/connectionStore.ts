import type { TunnelState } from '../types';

export type ConnectionStage = 'dns' | 'vk' | 'turn' | 'wrap' | 'dtls' | 'workers' | 'vpn';
export type ConnectionStageState = 'pending' | 'running' | 'success' | 'warning' | 'error';

export interface ConnectionProgress {
  stage: ConnectionStage;
  state: ConnectionStageState;
  message?: string;
}

export interface ConnectionDashboardState {
  state: TunnelState;
  stage: ConnectionStage | null;
  stages: Record<ConnectionStage, ConnectionStageState>;
  message: string;
  activeWorkers: number;
  totalWorkers: number;
  bytesUp: number;
  bytesDown: number;
  connectedAt: number | null;
  lastError: string | null;
}

type Listener = (state: ConnectionDashboardState) => void;

export const CONNECTION_STAGES: ConnectionStage[] = ['dns', 'vk', 'turn', 'wrap', 'dtls', 'workers', 'vpn'];

function emptyStages(): Record<ConnectionStage, ConnectionStageState> {
  return {
    dns: 'pending',
    vk: 'pending',
    turn: 'pending',
    wrap: 'pending',
    dtls: 'pending',
    workers: 'pending',
    vpn: 'pending',
  };
}

function initialState(): ConnectionDashboardState {
  return {
    state: 'idle',
    stage: null,
    stages: emptyStages(),
    message: '',
    activeWorkers: 0,
    totalWorkers: 0,
    bytesUp: 0,
    bytesDown: 0,
    connectedAt: null,
    lastError: null,
  };
}

let state = initialState();
const listeners = new Set<Listener>();

function publish(next: ConnectionDashboardState) {
  state = next;
  listeners.forEach(listener => listener(state));
}

function update(patch: Partial<ConnectionDashboardState>) {
  publish({ ...state, ...patch });
}

export const connectionStore = {
  get: () => state,

  begin(totalWorkers: number) {
    publish({
      ...initialState(),
      state: 'connecting',
      stage: 'dns',
      message: 'Разрешение адреса сервера',
      totalWorkers,
      stages: { ...emptyStages(), dns: 'running' },
    });
  },

  setTunnelState(next: TunnelState) {
    if (next === 'connected') {
      connectionStore.connected();
      return;
    }
    if (next === 'disconnecting') {
      update({ state: 'disconnecting', message: 'Отключение туннеля' });
      return;
    }
    if (next === 'idle') {
      connectionStore.disconnected();
      return;
    }
    update({ state: next });
  },

  progress(progress: ConnectionProgress) {
    const current = state.stages[progress.stage];
    if (current === 'success' && progress.state !== 'success') return;

    const stages = { ...state.stages, [progress.stage]: progress.state };
    update({
      stages,
      stage: progress.state === 'running' || progress.state === 'warning' || progress.state === 'error'
        ? progress.stage
        : state.stage,
      message: progress.message ?? state.message,
      lastError: progress.state === 'error' ? (progress.message || 'Ошибка подключения') : state.lastError,
    });
  },

  stats(activeWorkers: number, bytesUp: number, bytesDown: number) {
    update({
      activeWorkers: Math.max(0, activeWorkers),
      bytesUp: Math.max(0, bytesUp),
      bytesDown: Math.max(0, bytesDown),
    });
  },

  setError(message: string) {
    const stages = { ...state.stages };
    if (state.stage && stages[state.stage] !== 'success') stages[state.stage] = 'error';
    update({ stages, lastError: message, message });
  },

  fail(message: string) {
    connectionStore.setError(message);
    update({ state: 'idle', activeWorkers: 0 });
  },

  connected() {
    update({
      state: 'connected',
      stage: 'vpn',
      stages: { ...state.stages, vpn: 'success' },
      message: 'Туннель активен',
      connectedAt: state.connectedAt ?? Date.now(),
      lastError: null,
    });
  },

  disconnected() {
    if (state.lastError) {
      update({ state: 'idle', activeWorkers: 0, connectedAt: null });
      return;
    }
    publish({ ...initialState(), totalWorkers: state.totalWorkers });
  },

  reset() {
    publish(initialState());
  },

  subscribe(listener: Listener) {
    listeners.add(listener);
    listener(state);
    return () => { listeners.delete(listener); };
  },
};
