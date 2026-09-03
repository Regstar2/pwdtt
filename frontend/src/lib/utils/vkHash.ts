const MIN_VK_HASH_LENGTH = 16;
const HASH_PATTERN = /^[A-Za-z0-9_-]+$/;
const VK_HOSTS = new Set(['vk.com', 'vk.ru', 'm.vk.com', 'm.vk.ru']);

export function normalizeVKHash(raw: string): string {
  let value = raw.trim();
  if (!value) return '';

  if (/^https?:\/\//i.test(value)) {
    let parsed: URL;
    try {
      parsed = new URL(value);
    } catch {
      throw new Error('Некорректная ссылка VK');
    }
    if (!VK_HOSTS.has(parsed.hostname.toLowerCase())) {
      throw new Error('Ссылка должна вести на VK');
    }
    const marker = '/call/join/';
    const idx = parsed.pathname.toLowerCase().indexOf(marker);
    if (idx === -1) {
      throw new Error('В ссылке нет VK call hash');
    }
    value = parsed.pathname.slice(idx + marker.length).split('/')[0].trim();
  }

  value = value.split(/[?#/]/)[0].trim();
  if (!value) return '';
  if (value.length < MIN_VK_HASH_LENGTH) {
    throw new Error(`VK-хеш слишком короткий: минимум ${MIN_VK_HASH_LENGTH} символов`);
  }
  if (!HASH_PATTERN.test(value)) {
    throw new Error('VK-хеш содержит недопустимые символы');
  }
  return value;
}

export function normalizeVKHashes(values: readonly string[]): string[] {
  return values.map(normalizeVKHash);
}

export function hasDuplicateVKHashes(values: readonly string[]): boolean {
  const nonEmpty = values.filter(Boolean);
  return new Set(nonEmpty).size !== nonEmpty.length;
}

export function insertGeneratedVKHash(
  values: [string, string, string, string],
  hash: string,
): [string, string, string, string] {
  const normalized = normalizeVKHash(hash);
  const current = values.map(value => {
    try {
      return normalizeVKHash(value);
    } catch {
      return value.trim();
    }
  });
  if (current.includes(normalized)) return values;

  const index = values.findIndex(value => value.trim() === '');
  if (index === -1) return values;
  const next = [...values] as [string, string, string, string];
  next[index] = normalized;
  return next;
}
