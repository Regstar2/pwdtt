// Defense in depth: UI events can originate outside the Go session journal.
// Strip sensitive values before storing, displaying or copying log messages.
const encodedURL = /(?:https?|wss?|vkcalls?|wdtt|tg)%3a%2f%2f[^\s"'<>]+/gi;
const doubleEncodedURL = /(?:https?|wss?)%253a%252f%252f[^\s"'<>]+/gi;
const rawURL = /(?:\b(?:https?|wss?|vkcalls?|wdtt|tg):\/\/|\bvk\.com\/call\/(?:join\/)?)[^\s"'<>]+/gi;
const encodedField = /((?:%22)?(?:anonymname|anonymous_name|user_id|device_id|access_token|password|hash|hash_id|call_hash|hashid|deviceid|userid|anonym_name|profilename|session_token|username|profile_name|link)(?:%22)?%(?:3a|3d))[^&\s"'<>]+/gi;
const windowsPath = /(?:[a-z]:[\\/]|\\\\)[^"'<>,;|\r\n]+/gi;
const unixPath = /(?:\/home\/|\/Users\/|\/root\/|\/tmp\/|\/var\/folders\/)[^"'<>,;|\s]+/g;
const privateKeys = 'access_token|anonymous_token|session_key|session_token|success_token|client_secret|credential|password|secret|device_id|user_id|token|hash|call_hash|hash_id|hashid|deviceid|userid|sessionid|anonym_name|anonymname|anonymous_name|anonymousname|username|user_name|hostname|first_name|last_name|display_name|full_name|profile_name|profilename|server|link';
const quotedKeyValue = new RegExp('("?(?:' + privateKeys + ')"?\\s*[:=]\\s*)"[^"]*"', 'gi');
const plainKeyValue = new RegExp('("?(?:' + privateKeys + ')"?\\s*[:=]\\s*"?)([^"\\s,}\\]&]+)', 'gi');
const queryValues = /([?&][a-z_][a-z0-9_.-]*=)[^&\s"'<>]+/gi;

export function sanitizeLogText(value: string): string {
  return value
    .replace(encodedURL, '[link redacted]')
    .replace(doubleEncodedURL, '[link redacted]')
    .replace(rawURL, '[link redacted]')
    .replace(encodedField, '$1[redacted]')
    .replace(windowsPath, '[path redacted]')
    .replace(unixPath, '[path redacted]')
    .replace(quotedKeyValue, '$1"[redacted]"')
    .replace(plainKeyValue, '$1[redacted]')
    .replace(queryValues, '$1[redacted]');
}
