import { useEffect, useState } from 'react';
import { OpenCaptcha, CancelCaptcha } from '../../../wailsjs/go/backend/App';
import './CaptchaPrompt.css';

export interface CaptchaChallenge {
  requestId: string;
  mode: string;
  deadlineMs: number;
}

export function parseCaptchaChallenge(payload: unknown): CaptchaChallenge | null {
  try {
    const data = typeof payload === 'string' ? JSON.parse(payload) : payload;
    if (!data || typeof data !== 'object') return null;
    const record = data as Record<string, unknown>;
    if (typeof record.requestId !== 'string' || !/^[\da-f-]{36}$/i.test(record.requestId)) return null;
    if (typeof record.deadlineMs !== 'number' || !Number.isFinite(record.deadlineMs)) return null;
    return { requestId: record.requestId, deadlineMs: record.deadlineMs, mode: String(record.mode ?? 'manual') };
  } catch {
    return null;
  }
}

export default function CaptchaPrompt({ challenge, onDisconnect }: {
  challenge: CaptchaChallenge;
  onDisconnect: () => void;
}) {
  const [now, setNow] = useState(Date.now());
  const [opening, setOpening] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    const interval = window.setInterval(() => setNow(Date.now()), 250);
    return () => window.clearInterval(interval);
  }, []);

  useEffect(() => {
    setOpening(false);
    setError('');
    setNow(Date.now());
  }, [challenge.requestId]);

  const seconds = Math.max(0, Math.ceil((challenge.deadlineMs - now) / 1000));
  const expired = seconds === 0;

  const open = async () => {
    if (opening || expired) return;
    setOpening(true);
    setError('');
    try {
      // Remains pending until the user solves VK's actual challenge in Edge.
      await OpenCaptcha(challenge.requestId);
    } catch (cause: unknown) {
      setError(cause instanceof Error ? cause.message : String(cause));
    } finally {
      setOpening(false);
    }
  };

  const cancel = async () => {
    await CancelCaptcha(challenge.requestId);
  };

  return (
    <section className="captcha-backdrop" role="dialog" aria-modal="true" aria-labelledby="captcha-title">
      <div className="captcha-dialog">
        <h2 id="captcha-title">Требуется CAPTCHA VK</h2>
        <p>Откройте официальную CAPTCHA в Microsoft Edge и решите её вручную. Приложение получит подтверждение от VK автоматически.</p>
        <p className="captcha-time" role="timer">Осталось: {seconds} с</p>
        {opening && <p role="status">Ожидание решения CAPTCHA в Edge. Не закрывайте окно до подтверждения.</p>}
        {error && <p className="captcha-error" role="alert">{error}</p>}
        {expired && <p className="captcha-error" role="alert">Время ожидания CAPTCHA истекло.</p>}
        <div className="captcha-actions">
          <button type="button" onClick={() => void open()} disabled={opening || expired}>Открыть CAPTCHA VK</button>
          <button type="button" className="captcha-secondary" onClick={() => void cancel()} disabled={expired}>Отменить CAPTCHA</button>
          <button type="button" className="captcha-secondary" onClick={onDisconnect}>Отключиться</button>
        </div>
      </div>
    </section>
  );
}
