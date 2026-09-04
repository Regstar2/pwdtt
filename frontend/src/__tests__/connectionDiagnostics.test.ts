import { beforeEach, describe, expect, it } from 'vitest';
import { buildConnectionDiagnostics, getLogSubsystem } from '../lib/connectionDiagnostics';
import { logStore, type LogEntry } from '../lib/stores/logStore';

function entry(id: number, level: LogEntry['level'], message: string, count = 1): LogEntry {
  return { id, level, message, count, time: '12:00:00' };
}

describe('connection diagnostics', () => {
  it('extracts worker and traffic stats without inventing route status', () => {
    const result = buildConnectionDiagnostics([
      entry(1, 'INFO', '[STATS] Активных: 7, Трафик: 24.8 МБ'),
      entry(2, 'INFO', '[WG] Конфиг применён, туннель активен'),
    ], 'connected');

    expect(result.activeWorkers).toBe(7);
    expect(result.traffic).toBe('24.8 МБ');
    expect(result.wireGuard).toBe('ok');
    expect(result.ipv4).toBe('unknown');
    expect(result.ipv6).toBe('unknown');
  });

  it('recognises explicit IPv4 and IPv6 protection results', () => {
    const result = buildConnectionDiagnostics([
      entry(1, 'INFO', '[ROUTING] IPv4: через туннель'),
      entry(2, 'INFO', '[ROUTING] IPv6 leak protection: OK'),
    ], 'connected');

    expect(result.ipv4).toBe('ok');
    expect(result.ipv6).toBe('ok');
  });

  it('classifies common subsystems', () => {
    expect(getLogSubsystem('[ВОРКЕР #3] Ошибка Reader: EOF')).toBe('Worker');
    expect(getLogSubsystem('[WG] Конфиг применён')).toBe('WireGuard');
    expect(getLogSubsystem('[DNS] check OK')).toBe('DNS');
  });
});

describe('log grouping', () => {
  beforeEach(() => logStore.clear());

  it('groups the same worker error across worker ids', () => {
    logStore.push('ERROR', '[ВОРКЕР #2] Ошибка Reader: EOF');
    logStore.push('ERROR', '[ВОРКЕР #3] Ошибка Reader: EOF');

    const rows = logStore.getAll();
    expect(rows).toHaveLength(1);
    expect(rows[0].count).toBe(2);
    expect(rows[0].message).toContain('#3');
  });

  it('keeps different worker errors separate', () => {
    logStore.push('ERROR', '[ВОРКЕР #2] Ошибка Reader: EOF');
    logStore.push('ERROR', '[ВОРКЕР #3] Ошибка Writer: timeout');

    expect(logStore.getAll()).toHaveLength(2);
  });
});
