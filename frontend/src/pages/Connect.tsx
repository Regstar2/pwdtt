import { useEffect, useRef, useState } from 'react';
import type { MouseEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import { IconPlus } from '@tabler/icons-react';
import AddServer from '../modals/Add-server';
import EditServer from '../modals/Edit-server';
import { serverStore, settingsStore } from '../lib/store';
import { tunnelStore } from '../lib/stores/tunnelStore';
import { connectionStore } from '../lib/stores/connectionStore';
import { themeStore } from '../lib/stores/themeStore';
import { toastStore } from '../lib/stores/toastStore';
import { logStore } from '../lib/stores/logStore';
import { wdttLinkStore } from '../lib/utils/wdttLink';
import { getServerVKHashPolicy } from '../lib/utils/vkHashPolicy';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { MeasureLatency, SaveProfile } from '../../wailsjs/go/backend/App';
import { backend } from '../../wailsjs/go/models';
import type { Server, TunnelState, VKHashMode } from '../lib/types';
import { Connect as WailsConnect, Disconnect as WailsDisconnect, ListProfiles } from '../../wailsjs/go/backend/App';
import ConnectionHero from '../components/connect/ConnectionHero';
import ConnectionProgress from '../components/connect/ConnectionProgress';
import ConnectionStats from '../components/connect/ConnectionStats';
import ServerSelector from '../components/connect/ServerSelector';
import { SERVER_ICONS } from '../components/connect/ServerIcon';
import './Connect.css';

function requestedWorkers(server: Server, hashCount: number) {
  let workers = server.power || Math.max(9, hashCount * 9);
  workers = Math.max(9, Math.min(108, workers));
  return Math.floor(workers / 9) * 9;
}

function IconPicker({
  iconMenu,
  onPick,
  onClose,
}: {
  iconMenu: { server: Server; x: number; y: number };
  onPick: (key: string) => void;
  onClose: () => void;
}) {
  return (
    <>
      <button type="button" aria-label="Закрыть" className="icon-picker-backdrop" onClick={onClose} />
      <div
        className="icon-picker"
        style={{
          left: Math.min(iconMenu.x, window.innerWidth - 256),
          top: Math.max(12, iconMenu.y - 4 - (Math.ceil(SERVER_ICONS.length / 6) * 40 + 20)),
        }}
      >
        {SERVER_ICONS.map(icon => (
          <button
            type="button"
            key={icon.key}
            className={`icon-picker-btn${(iconMenu.server.icon ?? 'clover') === icon.key ? ' icon-picker-btn--active' : ''}`}
            onClick={() => onPick(icon.key)}
            title={icon.key}
          >
            {icon.render(18)}
          </button>
        ))}
      </div>
    </>
  );
}

export default function Connect() {
  const navigate = useNavigate();
  const [servers, setServers] = useState<Server[]>(() => serverStore.getAll());
  const [selected, setSelected] = useState<Server | null>(() => {
    const all = serverStore.getAll();
    if (all.length === 0) return null;
    const lastId = serverStore.getLastSelectedId();
    return all.find(server => server.id === lastId) ?? all[0];
  });
  const [listOpen, setListOpen] = useState(false);
  const [tunnelState, setTunnelState] = useState<TunnelState>(() => tunnelStore.get());
  const [connection, setConnection] = useState(() => connectionStore.get());
  const [theme, setTheme] = useState(() => themeStore.get());
  const [addServerOpen, setAddServerOpen] = useState(false);
  const [editServer, setEditServer] = useState<Server | null>(null);
  const [iconMenu, setIconMenu] = useState<{ server: Server; x: number; y: number } | null>(null);
  const [linkFlash, setLinkFlash] = useState(false);
  const [latencies, setLatencies] = useState<Record<string, number | null | undefined>>({});

  const selectedRef = useRef(selected);
  const linkFlashTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const connectingRef = useRef(false);
  const reconnectAtRef = useRef(0);

  useEffect(() => tunnelStore.subscribe(setTunnelState), []);
  useEffect(() => connectionStore.subscribe(setConnection), []);
  useEffect(() => themeStore.subscribe(setTheme), []);

  useEffect(() => EventsOn('vk-hash-replaced', (payload: unknown) => {
    if (!payload || typeof payload !== 'object') return;
    const data = payload as Record<string, unknown>;
    const profileName = String(data.profileName ?? '');
    const oldHash = String(data.oldHash ?? '');
    const newHash = String(data.newHash ?? '');
    const scope = String(data.scope ?? '');
    if (!profileName || !oldHash || !newHash || !scope.includes('local')) return;

    const target = serverStore.getAll().find(server => server.name === profileName);
    if (!target) return;
    const nextHashes = [...(target.hashes ?? ['', '', '', ''])] as [string, string, string, string];
    let changed = false;
    for (let i = 0; i < nextHashes.length; i++) {
      if (nextHashes[i].trim() === oldHash) {
        nextHashes[i] = newHash;
        changed = true;
      }
    }
    if (!changed) return;

    const updated = { ...target, hashes: nextHashes };
    serverStore.update(updated);
    const all = serverStore.getAll();
    setServers(all);
    setSelected(previous => previous?.id === updated.id ? { ...updated } : previous);
    toastStore.show(`VK-хеш профиля «${profileName}» автоматически заменён`, 4000);
  }), []);

  useEffect(() => {
    selectedRef.current = selected;
    serverStore.setLastSelectedId(selected?.id ?? null);
  }, [selected]);

  useEffect(() => () => {
    if (linkFlashTimerRef.current) clearTimeout(linkFlashTimerRef.current);
  }, []);

  const latencyTargetsKey = servers.map(server => `${server.id}:${server.host}`).join('|');
  useEffect(() => {
    if (servers.length === 0 || tunnelState !== 'idle') return;

    let cancelled = false;

    const measureAll = async () => {
      const targets = serverStore.getAll();
      const results = await Promise.all(targets.map(async server => {
        try {
          const value = await MeasureLatency(server.host);
          return [server.id, value >= 0 ? value : null] as const;
        } catch {
          return [server.id, null] as const;
        }
      }));

      if (cancelled) return;
      const next: Record<string, number | null | undefined> = {};
      for (const [id, value] of results) next[id] = value;
      setLatencies(next);
    };

    void measureAll();
    const timer = window.setInterval(() => { void measureAll(); }, 30000);

    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [latencyTargetsKey, tunnelState]);

  useEffect(() => {
    ListProfiles().then(profiles => {
      if (!profiles) return;
      const existing = serverStore.getAll();
      const existingNames = new Set(existing.map(server => server.name));
      const existingHosts = new Set(existing.map(server => server.host));
      let changed = false;

      for (const [name, profile] of Object.entries(profiles)) {
        if (existingNames.has(name)) continue;
        const host = profile.peer || '';
        if (!host || existingHosts.has(host)) continue;
        const hashes: [string, string, string, string] = [
          profile.hashes?.[0] ?? '',
          profile.hashes?.[1] ?? '',
          profile.hashes?.[2] ?? '',
          profile.hashes?.[3] ?? '',
        ];
        serverStore.add({
          name,
          host,
          password: profile.password ?? '',
          hashes,
          hashMode: (profile.hash_policy?.mode as VKHashMode | undefined) ?? 'local',
          hashAutoCheck: profile.hash_policy?.autoCheck ?? true,
          hashAutoReplace: profile.hash_policy?.autoReplace ?? false,
        });
        changed = true;
      }

      if (changed) {
        const all = serverStore.getAll();
        setServers(all);
        setSelected(previous => previous ?? all[0] ?? null);
      }
    }).catch(() => {});
  }, []);

  useEffect(() => wdttLinkStore.subscribe(link => {
    if (!link) return;
    const consumed = wdttLinkStore.consume();
    if (!consumed) return;

    const applyLink = async () => {
      const hashes = consumed.hashes.slice(0, 4);
      const padded: [string, string, string, string] = [hashes[0] ?? '', hashes[1] ?? '', hashes[2] ?? '', hashes[3] ?? ''];
      const existingNames = serverStore.getAll().map(server => server.name);
      let autoName = consumed.name || 'Сервер';
      if (autoName === 'Server') autoName = 'Сервер';
      let counter = 1;
      while (existingNames.includes(`${autoName} ${counter}`)) counter++;
      const name = `${autoName} ${counter}`;

      await SaveProfile(name, backend.ProfileData.createFrom({
        peer: consumed.host,
        password: consumed.password,
        hashes: hashes as unknown as string[],
        turn: '',
        port: consumed.port || '',
        device_id: '',
        listen: '',
      }));

      const server = serverStore.add({
        name,
        host: consumed.host,
        password: consumed.password,
        hashes: padded,
        power: consumed.workers,
      });
      setServers(serverStore.getAll());
      setSelected({ ...server });
      setLinkFlash(true);
      if (linkFlashTimerRef.current) clearTimeout(linkFlashTimerRef.current);
      linkFlashTimerRef.current = setTimeout(() => setLinkFlash(false), 800);
      toastStore.show(`Профиль добавлен: ${name}`, 3000);
    };

    void applyLink();
  }), []);

  const doConnect = async () => {
    const current = selectedRef.current;
    if (!current || tunnelStore.get() !== 'idle' || connectingRef.current) return;

    const hashes = (current.hashes ?? []).filter(hash => hash.trim());
    const hashPolicy = getServerVKHashPolicy(current);
    if (hashPolicy.mode === 'local' && hashes.length === 0) {
      toastStore.show('Добавьте локальные VK-хеши или включите общий пул');
      return;
    }

    connectingRef.current = true;
    const workers = requestedWorkers(current, Math.max(hashes.length, 1));
    connectionStore.begin(workers);
    tunnelStore.set('connecting');
    logStore.clear();

    try {
      await WailsConnect({
        peerAddr: current.host,
        password: current.password,
        hashes,
        deviceId: current.deviceId,
        workers,
        captchaMode: 'auto',
        obfsMode: settingsStore.get().obfsMode || 'audio',
        profileName: current.name,
        hashMode: hashPolicy.mode,
        hashAutoCheck: hashPolicy.autoCheck,
        hashAutoReplace: hashPolicy.autoReplace,
      });
    } catch (error: unknown) {
      const message = error instanceof Error ? error.message : String(error);
      logStore.push('ERROR', message);
      toastStore.show(message, 6000);
      tunnelStore.set('idle');
      connectionStore.fail(message);
    } finally {
      connectingRef.current = false;
    }
  };

  const handleTunnel = async () => {
    if (!selectedRef.current) return;

    if (tunnelState === 'idle') {
      if (Date.now() < reconnectAtRef.current) {
        const seconds = Math.ceil((reconnectAtRef.current - Date.now()) / 1000);
        toastStore.show(`Подождите ${seconds} сек.`, 2000);
        return;
      }
      toastStore.show('Убедитесь что другие VPN отключены', 4000);
      await doConnect();
      return;
    }

    if (tunnelState === 'connected' || tunnelState === 'connecting') {
      tunnelStore.set('disconnecting');
      connectionStore.setTunnelState('disconnecting');
      try {
        await WailsDisconnect();
      } catch {
      } finally {
        tunnelStore.set('idle');
        connectionStore.disconnected();
        reconnectAtRef.current = Date.now() + 4000;
      }
    }
  };

  const handleAdd = (data: Omit<Server, 'id'>) => {
    const server = serverStore.add(data);
    setServers(serverStore.getAll());
    setSelected(server);
  };

  const handleSave = (server: Server) => {
    serverStore.update(server);
    const all = serverStore.getAll();
    setServers(all);
    if (selected?.id === server.id) setSelected(server);
  };

  const handleDelete = (id: string) => {
    serverStore.remove(id);
    const all = serverStore.getAll();
    setServers(all);
    if (selected?.id === id) setSelected(all[0] ?? null);
  };

  const handleIconClick = (event: MouseEvent<HTMLButtonElement>, server: Server) => {
    event.stopPropagation();
    const rect = event.currentTarget.getBoundingClientRect();
    setIconMenu({ server, x: rect.left, y: rect.top });
  };

  const handlePickIcon = (key: string) => {
    if (!iconMenu) return;
    const updated = { ...iconMenu.server, icon: key };
    serverStore.update(updated);
    const all = serverStore.getAll();
    setServers(all);
    if (selected?.id === iconMenu.server.id) setSelected(updated);
    setIconMenu(null);
  };

  const obfsMode = settingsStore.get().obfsMode;

  return (
    <main className="connect-page">
      <div className="connection-dashboard">
        <div className="server-toolbar">
          <ServerSelector
            servers={servers}
            selected={selected}
            listOpen={listOpen}
            obfsMode={obfsMode}
            latencies={latencies}
            onToggleList={() => setListOpen(open => !open)}
            onSelect={server => { setSelected({ ...server }); setListOpen(false); }}
            onIconClick={handleIconClick}
            onEdit={server => setEditServer(server)}
          />

          <button
            type="button"
            className="btn-add"
            onClick={() => setAddServerOpen(true)}
            aria-label="Добавить сервер"
            title="Добавить сервер"
          >
            <IconPlus stroke={2} size={22} />
          </button>
        </div>

        <ConnectionHero
          theme={theme}
          connection={connection}
          selected={selected}
          linkFlash={linkFlash}
          onTunnel={handleTunnel}
          onRetry={doConnect}
          onLogs={() => navigate('/logs')}
        />

        <ConnectionProgress connection={connection} />

        <ConnectionStats
          connection={connection}
          latency={selected ? latencies[selected.id] : undefined}
          onOpenLogs={() => navigate('/logs')}
        />
      </div>

      {addServerOpen && <AddServer onClose={() => setAddServerOpen(false)} onAdd={handleAdd} />}
      {editServer && (
        <EditServer
          server={editServer}
          onClose={() => setEditServer(null)}
          onSave={handleSave}
          onDelete={handleDelete}
        />
      )}
      {iconMenu && (
        <IconPicker
          iconMenu={iconMenu}
          onPick={handlePickIcon}
          onClose={() => setIconMenu(null)}
        />
      )}
    </main>
  );
}
