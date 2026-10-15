// @vitest-environment happy-dom

import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import ConnectionHero from '../components/connect/ConnectionHero';
import { connectionStore } from '../lib/stores/connectionStore';
import type { Server } from '../lib/types';

const server: Server = { id: 'test-server', name: 'Test VPN', host: '192.0.2.1:443', password: 'test' };
const dnsError = 'Не удалось разрешить адрес VK API / проблема DNS';

describe('ConnectionHero VK credentials failure recovery', () => {
  let container: HTMLDivElement;
  let root: ReturnType<typeof createRoot>;

  beforeEach(() => {
    connectionStore.reset();
    container = document.createElement('div');
    document.body.appendChild(container);
    root = createRoot(container);
  });

  afterEach(() => {
    act(() => root.unmount());
    container.remove();
    connectionStore.reset();
  });

  it('renders the DNS error and routes the Retry click to Connect', () => {
    const onRetry = vi.fn();
    const onTunnel = vi.fn();
    const onLogs = vi.fn();

    connectionStore.begin(9);
    connectionStore.progress({ stage: 'vk', state: 'running' });
    connectionStore.progress({ stage: 'vk', state: 'error', message: dnsError });
    connectionStore.disconnected();

    act(() => root.render(
      <ConnectionHero
        theme="dark"
        selected={server}
        connection={connectionStore.get()}
        linkFlash={false}
        onTunnel={onTunnel}
        onRetry={onRetry}
        onLogs={onLogs}
      />,
    ));

    expect(container.textContent).toContain('Не удалось подключиться');
    expect(container.textContent).toContain(dnsError);

    const retry = [...container.querySelectorAll('button')].find(button => button.textContent === 'Повторить');
    expect(retry).toBeTruthy();
    act(() => retry!.dispatchEvent(new MouseEvent('click', { bubbles: true })));
    expect(onRetry).toHaveBeenCalledTimes(1);
    expect(onTunnel).not.toHaveBeenCalled();

    // The next attempt resets the error and returns to a connecting UI.
    connectionStore.begin(9);
    act(() => root.render(
      <ConnectionHero
        theme="dark"
        selected={server}
        connection={connectionStore.get()}
        linkFlash={false}
        onTunnel={onTunnel}
        onRetry={onRetry}
        onLogs={onLogs}
      />,
    ));
    expect(container.textContent).toContain('Подключение к серверу');
    expect(container.textContent).not.toContain(dnsError);
  });
});
