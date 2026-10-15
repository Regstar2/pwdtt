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
