import { describe, it, expect, beforeEach } from 'vitest';
import { logStore } from '../lib/stores/logStore';

beforeEach(() => {
  logStore.clear();
});

describe('logStore', () => {
  it('push: добавляет запись', () => {
    logStore.push('INFO', 'test message');

    const entries = logStore.getAll();
    expect(entries.length).toBe(1);
    expect(entries[0].level).toBe('INFO');
    expect(entries[0].message).toBe('test message');
    expect(entries[0].count).toBe(1);
    expect(entries[0].time).toBeTruthy();
  });

  it('push: ERROR всегда добавляет новую запись', () => {
    logStore.push('ERROR', 'error 1');
    logStore.push('ERROR', 'error 1');
    logStore.push('ERROR', 'error 1');

    const entries = logStore.getAll();
    expect(entries.length).toBe(3);
    for (const e of entries) {
      expect(e.level).toBe('ERROR');
      expect(e.count).toBe(1);
    }
  });

  it('push: DEBUG сохраняет каждую трассировочную запись', () => {
    logStore.push('DEBUG', '[HASH] stage=turn action=start');
    logStore.push('DEBUG', '[HASH] stage=turn action=complete');

    const entries = logStore.getAll();
    expect(entries.length).toBe(2);
    expect(entries[0].level).toBe('DEBUG');
    expect(entries[1].level).toBe('DEBUG');
  });

  it('push: дубликаты увеличивают count', () => {
    logStore.push('INFO', 'same message');
    logStore.push('INFO', 'same message');
    logStore.push('INFO', 'same message');

    const entries = logStore.getAll();
    expect(entries.length).toBe(1);
    expect(entries[0].count).toBe(3);
  });

  it('push: разные события одного subsystem не схлопываются', () => {
    logStore.push('INFO', '[WG] Connecting...');
    logStore.push('INFO', '[WG] Connected!');

    const entries = logStore.getAll();
    expect(entries.length).toBe(2);
    expect(entries[0].message).toBe('[WG] Connecting...');
    expect(entries[1].message).toBe('[WG] Connected!');
    expect(entries[0].count).toBe(1);
    expect(entries[1].count).toBe(1);
  });

  it('push: повторённое событие перемещается в конец хронологии', () => {
    logStore.push('INFO', '[ЯДРО] Рабочая DTLS-сессия установлена');
    logStore.push('DEBUG', '[CORE] middle');
    logStore.push('INFO', '[ЯДРО] Рабочая DTLS-сессия установлена');

    const entries = logStore.getAll();
    expect(entries.length).toBe(2);
    expect(entries[0].message).toBe('[CORE] middle');
    expect(entries[1].message).toBe('[ЯДРО] Рабочая DTLS-сессия установлена');
    expect(entries[1].count).toBe(2);
  });

  it('push: разные worker события сохраняются отдельно', () => {
    logStore.push('INFO', '[Worker #1] Running');
    logStore.push('INFO', '[Worker #2] Running');

    const entries = logStore.getAll();
    expect(entries.length).toBe(2);
  });

  it('push: разные сообщения Peer сохраняются отдельно', () => {
    logStore.push('INFO', '[Peer] Connected 1');
    logStore.push('INFO', '[Peer] Connected 2');

    const entries = logStore.getAll();
    expect(entries.length).toBe(2);
  });

  it('push: STATS остаётся обновляемым snapshot', () => {
    logStore.push('INFO', '[STATS] Активных: 8, Трафик: 0.08 МБ');
    logStore.push('DEBUG', '[CORE] between stats');
    logStore.push('INFO', '[STATS] Активных: 9, Трафик: 0.10 МБ');

    const entries = logStore.getAll();
    expect(entries.length).toBe(2);
    expect(entries[0].message).toBe('[CORE] between stats');
    expect(entries[1].message).toBe('[STATS] Активных: 9, Трафик: 0.10 МБ');
    expect(entries[1].count).toBe(2);
  });

  it('clear: очищает все записи', () => {
    logStore.push('INFO', 'msg1');
    logStore.push('ERROR', 'msg2');
    logStore.clear();

    expect(logStore.getAll()).toEqual([]);
  });

  it('MAX_ENTRIES: обрезка при превышении', () => {
    // Push 505 записей — должна остаться 500
    for (let i = 0; i < 505; i++) {
      logStore.push('INFO', `message ${i}`);
    }

    const entries = logStore.getAll();
    expect(entries.length).toBe(500);
    // Первые 5 должны быть обрезаны
    expect(entries[0].message).toBe('message 5');
  });

  it('extractTag: нет тега → пустая строка', () => {
    logStore.push('INFO', 'no tag here');
    // Запись добавляется без тега — дубликаты не группируются
    logStore.push('INFO', 'no tag here');

    // Последняя проверка — дубликат без тега группируется по message
    const entries = logStore.getAll();
    expect(entries.length).toBe(1);
    expect(entries[0].count).toBe(2);
  });

  it('subscribe: вызывает listener сразу', () => {
    logStore.push('INFO', 'test');
    const entries: any[][] = [];
    const unsub = logStore.subscribe(e => entries.push(e));

    expect(entries.length).toBe(1);
    expect(entries[0].length).toBe(1);
    unsub();
  });

  it('unsubscribe: отписка', () => {
    const entries: any[][] = [];
    const unsub = logStore.subscribe(e => entries.push(e));

    unsub();
    logStore.push('INFO', 'after unsub');

    expect(entries.length).toBe(1);
  });
});
