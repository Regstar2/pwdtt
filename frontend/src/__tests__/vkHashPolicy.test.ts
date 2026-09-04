import { describe, expect, it } from 'vitest';
import { getServerVKHashPolicy, hashModeUsesLocal, hashModeUsesPool } from '../lib/utils/vkHashPolicy';

describe('vkHashPolicy', () => {
  it('keeps legacy servers on local hashes with automatic checks', () => {
    expect(getServerVKHashPolicy({})).toEqual({
      mode: 'local',
      autoCheck: true,
      autoReplace: false,
    });
  });

  it('preserves explicit per-server settings', () => {
    expect(getServerVKHashPolicy({
      hashMode: 'local+pool',
      hashAutoCheck: false,
      hashAutoReplace: true,
    })).toEqual({
      mode: 'local+pool',
      autoCheck: false,
      autoReplace: true,
    });
  });

  it('describes local and pool participation', () => {
    expect(hashModeUsesLocal('local')).toBe(true);
    expect(hashModeUsesPool('local')).toBe(false);
    expect(hashModeUsesLocal('pool')).toBe(false);
    expect(hashModeUsesPool('pool')).toBe(true);
    expect(hashModeUsesLocal('local+pool')).toBe(true);
    expect(hashModeUsesPool('local+pool')).toBe(true);
  });
});
