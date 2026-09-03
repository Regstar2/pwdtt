import { describe, expect, it } from 'vitest';
import { hasDuplicateVKHashes, insertGeneratedVKHash, normalizeVKHash } from '../lib/utils/vkHash';

const HASH = 'AbCdEfGh12345678_xyz';

describe('normalizeVKHash', () => {
  it.each([
    [HASH, HASH],
    [`https://vk.com/call/join/${HASH}`, HASH],
    [`https://vk.ru/call/join/${HASH}`, HASH],
    [`https://m.vk.com/call/join/${HASH}`, HASH],
    [`https://m.vk.ru/call/join/${HASH}`, HASH],
    [`https://vk.com/call/join/${HASH}?from=test`, HASH],
    [`https://vk.ru/call/join/${HASH}#fragment`, HASH],
  ])('normalizes %s', (input, expected) => {
    expect(normalizeVKHash(input)).toBe(expected);
  });

  it('keeps empty input empty', () => {
    expect(normalizeVKHash('   ')).toBe('');
  });

  it.each([
    'short',
    'https://example.com/call/join/AbCdEfGh12345678',
    'https://vk.com/not-a-call/AbCdEfGh12345678',
    'AbCd EfGh12345678',
  ])('rejects invalid input %s', input => {
    expect(() => normalizeVKHash(input)).toThrow();
  });
});

describe('VK hash duplicates', () => {
  it('detects duplicates', () => {
    expect(hasDuplicateVKHashes([HASH, HASH, ''])).toBe(true);
    expect(hasDuplicateVKHashes([HASH, 'DifferentHash123456', ''])).toBe(false);
  });

  it('does not overwrite fields or insert duplicate generated hashes', () => {
    const values: [string, string, string, string] = [HASH, '', 'ManualHash1234567', ''];
    expect(insertGeneratedVKHash(values, HASH)).toEqual(values);
    expect(insertGeneratedVKHash(values, 'GeneratedHash123456')).toEqual([
      HASH,
      'GeneratedHash123456',
      'ManualHash1234567',
      '',
    ]);
  });
});
