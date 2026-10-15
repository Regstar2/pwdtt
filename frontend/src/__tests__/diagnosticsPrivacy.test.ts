import { beforeEach, describe, expect, it } from 'vitest';
import { sanitizeLogText } from '../lib/utils/diagnostics';
import { logStore } from '../lib/stores/logStore';

describe('issue #25 diagnostics privacy', () => {
  beforeEach(() => logStore.clear());

  const examples: Array<[string, string]> = [
    ['https://vk.com/call/join/PrivateJoinHash', 'PrivateJoinHash'],
    ['link=https%3A%2F%2Fvk.com%2Fcall%2Fjoin%2FPrivateJoinHash&anonymName=SecretName', 'PrivateJoinHash'],
    ['link%3Dhttps%253A%252F%252Fvk.com%252Fcall%252Fjoin%252FPrivateJoinHash', 'PrivateJoinHash'],
    ['anonymName=SecretName', 'SecretName'],
    ['anonymName%3DSecretName', 'SecretName'],
    ['{"anonymName":"Secret Name"}', 'Secret Name'],
    ['{"username":"Secret Name","user_id":123456}', 'Secret Name'],
    ['device_id=PrivateDeviceID', 'PrivateDeviceID'],
    ['hash_id=PrivateHashID', 'PrivateHashID'],
    ['server=PrivateProfile', 'PrivateProfile'],
    ['C:\\Users\\PrivateUser\\AppData\\Roaming\\pwdtt', 'PrivateUser'],
    ['C:/Users/PrivateUser/AppData/Roaming/pwdtt', 'PrivateUser'],
    ['/home/PrivateUser/.config/pwdtt', 'PrivateUser'],
  ];

  it.each(examples)('redacts "%s"', (input, secret) => {
    expect(sanitizeLogText(input)).not.toContain(secret);
    logStore.push('DEBUG', input);
    const stored = logStore.getAll()[0].message;
    expect(stored).not.toContain(secret);
    expect(stored).toContain('[redacted]');
  });

  it('preserves non-sensitive operational information', () => {
    const text = sanitizeLogText('[WG] workers=2 elapsed=34ms action=complete');
    expect(text).toContain('workers=2');
    expect(text).toContain('34ms');
    expect(text).toContain('action=complete');
  });

  it('sanitizes before subscribers receive an event', () => {
    const received: string[] = [];
    const unsubscribe = logStore.subscribe(entries => {
      for (const entry of entries) received.push(entry.message);
    });
    logStore.push('DEBUG', 'anonymName=NeverPublishThis');
    unsubscribe();
    expect(received.join(' ')).not.toContain('NeverPublishThis');
  });
});
