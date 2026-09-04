import { beforeEach, describe, expect, it, vi } from 'vitest';
import { connectionStore } from '../lib/stores/connectionStore';

describe('connectionStore', () => {
  beforeEach(() => {
    connectionStore.reset();
    vi.restoreAllMocks();
  });

  it('проходит idle → connecting → connected', () => {
    vi.spyOn(Date, 'now').mockReturnValue(123456);
    connectionStore.begin(18);

    expect(connectionStore.get().state).toBe('connecting');
    expect(connectionStore.get().stages.dns).toBe('running');
    expect(connectionStore.get().totalWorkers).toBe(18);

    connectionStore.progress({ stage: 'dns', state: 'success', message: 'DNS готов' });
    connectionStore.progress({ stage: 'vk', state: 'running', message: 'VK' });
    connectionStore.progress({ stage: 'vk', state: 'success', message: 'VK готов' });
    connectionStore.stats(3, 1024, 2048);
    connectionStore.connected();

    const state = connectionStore.get();
    expect(state.state).toBe('connected');
    expect(state.activeWorkers).toBe(3);
    expect(state.bytesUp).toBe(1024);
    expect(state.bytesDown).toBe(2048);
    expect(state.connectedAt).toBe(123456);
    expect(state.stages.vpn).toBe('success');
  });

  it('не откатывает успешно завершённый этап из-за параллельного воркера', () => {
    connectionStore.begin(18);
    connectionStore.progress({ stage: 'turn', state: 'success' });
    connectionStore.progress({ stage: 'turn', state: 'running' });
    connectionStore.progress({ stage: 'turn', state: 'warning' });

    expect(connectionStore.get().stages.turn).toBe('success');
  });

  it('сохраняет ошибку после disconnected до следующей попытки', () => {
    connectionStore.begin(18);
    connectionStore.progress({ stage: 'dtls', state: 'running' });
    connectionStore.setError('DTLS timeout');
    connectionStore.disconnected();

    expect(connectionStore.get().state).toBe('idle');
    expect(connectionStore.get().lastError).toBe('DTLS timeout');
    expect(connectionStore.get().stages.dtls).toBe('error');

    connectionStore.begin(18);
    expect(connectionStore.get().lastError).toBeNull();
    expect(connectionStore.get().stages.dtls).toBe('pending');
  });

  it('уведомляет подписчиков о статистике и состояниях', () => {
    const seen: string[] = [];
    const off = connectionStore.subscribe(state => seen.push(state.state));

    connectionStore.begin(9);
    connectionStore.stats(1, 10, 20);
    connectionStore.setTunnelState('disconnecting');
    connectionStore.disconnected();
    off();

    expect(seen).toContain('connecting');
    expect(seen).toContain('disconnecting');
    expect(seen.at(-1)).toBe('idle');
  });
});
