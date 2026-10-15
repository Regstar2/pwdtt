import { describe, expect, it } from 'vitest';
import { parseCaptchaChallenge } from '../components/connect/CaptchaPrompt';

describe('CAPTCHA UI event parsing', () => {
  const valid = { requestId: '84351ab4-1234-4abc-a123-1234567890ab', mode: 'manual', deadlineMs: 1760555000000 };

  it('accepts a correlated payload from the backend', () => {
    expect(parseCaptchaChallenge(JSON.stringify(valid))).toEqual(valid);
  });

  it('rejects an invalid request id and malformed events', () => {
    expect(parseCaptchaChallenge({ ...valid, requestId: '' })).toBeNull();
    expect(parseCaptchaChallenge({ ...valid, deadlineMs: 'tomorrow' })).toBeNull();
    expect(parseCaptchaChallenge('invalid JSON')).toBeNull();
  });

  it('does not require private CAPTCHA URL or token in the UI payload', () => {
    const result = parseCaptchaChallenge(valid);
    expect(result).toEqual(valid);
    expect(JSON.stringify(result)).not.toContain('session_token');
    expect(JSON.stringify(result)).not.toContain('success_token');
  });
});
