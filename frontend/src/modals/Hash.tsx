import { useEffect, useState } from 'react';
import { IconHash, IconLogin, IconLogout, IconPlus, IconWand, IconX } from '@tabler/icons-react';
import {
  CancelVKOperation,
  GenerateVKHashes,
  IsVKAuthAvailable,
  IsVKLoggedIn,
  VKLogin,
  VKLogout,
} from '../../wailsjs/go/backend/App';
import { EventsOn } from '../../wailsjs/runtime/runtime';
import { toastStore } from '../lib/stores/toastStore';
import {
  hasDuplicateVKHashes,
  insertGeneratedVKHash,
  normalizeVKHashes,
} from '../lib/utils/vkHash';

interface Props {
  hashes: [string, string, string, string];
  onClose: () => void;
  onSave: (hashes: [string, string, string, string]) => void;
}

type BusyAction = 'login' | 'logout' | 'generate' | null;

function errorMessage(error: unknown): string {
  const message = error instanceof Error ? error.message : String(error);
  return message.replace(/^Error:\s*/i, '') || 'Неизвестная ошибка VK';
}

export default function Hash({ hashes, onClose, onSave }: Props) {
  const [values, setValues] = useState<[string, string, string, string]>([...hashes] as [string, string, string, string]);
  const [authAvailable, setAuthAvailable] = useState(false);
  const [loggedIn, setLoggedIn] = useState(false);
  const [authChecked, setAuthChecked] = useState(false);
  const [busy, setBusy] = useState<BusyAction>(null);
  const [progress, setProgress] = useState<{ current: number; total: number } | null>(null);

  useEffect(() => {
    let active = true;
    Promise.all([IsVKAuthAvailable(), IsVKLoggedIn()])
      .then(([available, logged]) => {
        if (!active) return;
        setAuthAvailable(available);
        setLoggedIn(logged);
        setAuthChecked(true);
      })
      .catch(() => {
        if (active) setAuthChecked(true);
      });

    const offAuth = EventsOn('vk-auth-changed', (logged: boolean) => {
      setLoggedIn(Boolean(logged));
    });
    const offGenerated = EventsOn('vk-hash-generated', (hash: string, current: number, total: number) => {
      setValues(previous => insertGeneratedVKHash(previous, hash));
      setProgress({ current: Number(current), total: Number(total) });
    });

    return () => {
      active = false;
      offAuth();
      offGenerated();
    };
  }, []);

  const set = (i: number, v: string) => {
    const next = [...values] as [string, string, string, string];
    next[i] = v;
    setValues(next);
  };

  const save = () => {
    try {
      const normalized = normalizeVKHashes(values) as [string, string, string, string];
      if (hasDuplicateVKHashes(normalized)) {
        toastStore.show('Обнаружены дублирующиеся хеши');
        return;
      }
      onSave(normalized);
      onClose();
    } catch (error) {
      toastStore.show(errorMessage(error));
    }
  };

  const refreshAuth = async () => {
    try {
      setLoggedIn(await IsVKLoggedIn());
    } catch {
      setLoggedIn(false);
    }
  };

  const login = async () => {
    if (busy) return;
    setBusy('login');
    try {
      await VKLogin();
      await refreshAuth();
      toastStore.show('VK подключён');
    } catch (error) {
      toastStore.show(errorMessage(error));
      await refreshAuth();
    } finally {
      setBusy(null);
    }
  };

  const logout = async () => {
    if (busy) return;
    setBusy('logout');
    try {
      await VKLogout();
      setLoggedIn(false);
      toastStore.show('VK отключён');
    } catch (error) {
      toastStore.show(errorMessage(error));
    } finally {
      setBusy(null);
    }
  };

  const generate = async (requested: number) => {
    if (busy || !loggedIn) return;
    const free = values.filter(value => value.trim() === '').length;
    const count = Math.min(requested, free);
    if (count < 1) {
      toastStore.show('Все четыре поля уже заполнены');
      return;
    }

    setBusy('generate');
    setProgress({ current: 0, total: count });
    try {
      const existing = values.filter(value => value.trim() !== '');
      const generated = await GenerateVKHashes(count, existing);
      setValues(previous => generated.reduce(
        (current, hash) => insertGeneratedVKHash(current, hash),
        previous,
      ));
      toastStore.show(count === 1 ? 'VK-хеш создан' : `Создано VK-хешей: ${generated.length}`);
    } catch (error) {
      toastStore.show(errorMessage(error));
      await refreshAuth();
    } finally {
      setBusy(null);
      setProgress(null);
    }
  };

  const closeOrCancel = () => {
    if (busy) {
      void CancelVKOperation();
      return;
    }
    onClose();
  };

  const freeCount = values.filter(value => value.trim() === '').length;
  const generating = busy === 'generate';

  return (
    <>
      <style>{`
        .hash-overlay { position: fixed; inset: 0; background: var(--overlay-bg); backdrop-filter: blur(4px); display: flex; align-items: center; justify-content: center; z-index: 200; animation: overlay-in 0.3s ease-out; }
        .hash-modal { background: var(--surface); border-radius: 14px; padding: 20px; width: 380px; max-width: 95vw; box-shadow: var(--shadow); border: 1px solid var(--border); animation: modal-in 0.3s ease-out; }
        .hash-header { display: flex; align-items: center; gap: 10px; margin-bottom: 18px; color: var(--text); }
        .hash-title { font-size: 16px; font-weight: 600; flex: 1; color: var(--text); }
        .hash-close { background: none; border: none; cursor: pointer; font-size: 18px; color: var(--text); line-height: 1; padding: 0; }
        .hash-input { width: 100%; padding: 11px 14px; border: 1.5px solid var(--input-border); border-radius: 10px; font-size: 14px; font-family: 'Geist', sans-serif; outline: none; margin-bottom: 10px; box-sizing: border-box; color: var(--text); background: var(--input-bg); }
        .hash-input::placeholder { color: var(--text-4); }
        .hash-btn { width: 100%; padding: 13px; border: none; border-radius: 10px; background: var(--accent); color: var(--accent-fg); font-size: 14px; font-family: 'Geist', sans-serif; font-weight: 600; cursor: pointer; margin-top: 4px; }
        .hash-btn:disabled, .hash-action:disabled { opacity: 0.45; cursor: not-allowed; }
        .hash-vk { margin: 4px 0 14px; padding: 12px; border: 1px solid var(--border); border-radius: 10px; background: var(--bg-2); }
        .hash-vk-status { display: flex; align-items: center; justify-content: space-between; gap: 8px; margin-bottom: 10px; color: var(--text-3); font-size: 12px; }
        .hash-vk-state { color: var(--text); font-weight: 600; }
        .hash-actions { display: grid; grid-template-columns: 1fr 1fr; gap: 8px; }
        .hash-action { min-width: 0; padding: 10px 8px; border: 1px solid var(--border); border-radius: 9px; background: var(--surface); color: var(--text); font-size: 12px; font-family: 'Geist', sans-serif; font-weight: 600; cursor: pointer; display: flex; align-items: center; justify-content: center; gap: 6px; }
        .hash-action--wide { grid-column: 1 / -1; }
        .hash-progress { margin-top: 9px; color: var(--text-3); font-size: 12px; text-align: center; }
        .hash-unavailable { color: var(--text-4); font-size: 12px; line-height: 1.45; }
      `}</style>
      <div className="hash-overlay">
        <div className="hash-modal" onClick={e => e.stopPropagation()}>
          <div className="hash-header">
            <IconHash stroke={2} size={22} />
            <span className="hash-title">Hash</span>
            <button
              type="button"
              className="hash-close"
              onClick={closeOrCancel}
              aria-label={busy ? 'Отменить операцию VK' : 'Закрыть'}
              title={busy ? 'Отменить операцию VK' : 'Закрыть'}
            >
              <IconX size={18} />
            </button>
          </div>

          {values.map((v, i) => (
            <input
              key={['hash-a', 'hash-b', 'hash-c', 'hash-d'][i]}
              className="hash-input"
              placeholder={`Hash - ${i + 1}`}
              value={v}
              onChange={e => set(i, e.target.value)}
              disabled={generating}
            />
          ))}

          <div className="hash-vk">
            {!authChecked ? (
              <div className="hash-unavailable">Проверка VK…</div>
            ) : !authAvailable ? (
              <div className="hash-unavailable">
                Автогенерация VK-хешей доступна только в Windows. Ручной ввод продолжает работать.
              </div>
            ) : (
              <>
                <div className="hash-vk-status">
                  <span>VK</span>
                  <span className="hash-vk-state">{loggedIn ? 'Авторизован' : 'Не авторизован'}</span>
                </div>
                <div className="hash-actions">
                  {loggedIn ? (
                    <button type="button" className="hash-action hash-action--wide" onClick={logout} disabled={Boolean(busy)}>
                      <IconLogout size={15} />
                      {busy === 'logout' ? 'Выход…' : 'Выйти из VK'}
                    </button>
                  ) : (
                    <button type="button" className="hash-action hash-action--wide" onClick={login} disabled={Boolean(busy)}>
                      <IconLogin size={15} />
                      {busy === 'login' ? 'Ожидание VK…' : 'Войти в VK'}
                    </button>
                  )}
                  {loggedIn && (
                    <>
                      <button type="button" className="hash-action" onClick={() => generate(1)} disabled={Boolean(busy) || freeCount === 0}>
                        <IconPlus size={15} />
                        Создать 1 хеш
                      </button>
                      <button type="button" className="hash-action" onClick={() => generate(freeCount)} disabled={Boolean(busy) || freeCount === 0}>
                        <IconWand size={15} />
                        Заполнить свободные
                      </button>
                    </>
                  )}
                </div>
                {progress && (
                  <div className="hash-progress">
                    Создание звонков VK… {progress.current}/{progress.total}
                  </div>
                )}
              </>
            )}
          </div>

          <button type="button" className="hash-btn" onClick={save} disabled={Boolean(busy)}>Сохранить</button>
        </div>
      </div>
    </>
  );
}
