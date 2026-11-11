import { describe, expect, it } from 'vitest';
import { strings } from './strings';

// Walks nested objects too, so a future grouped section is checked as well.
function leaves(value: unknown, path: string): [string, unknown][] {
  if (value !== null && typeof value === 'object') {
    return Object.entries(value).flatMap(([k, v]) => leaves(v, path ? `${path}.${k}` : k));
  }
  return [[path, value]];
}

describe('strings', () => {
  it('has no empty values', () => {
    const all = leaves(strings, '');
    expect(all.length).toBeGreaterThan(0);
    for (const [key, value] of all) {
      expect(typeof value, key).toBe('string');
      expect((value as string).trim(), key).not.toBe('');
    }
  });
});
