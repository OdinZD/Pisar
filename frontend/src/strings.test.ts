import { describe, expect, it } from 'vitest';
import { strings } from './strings';

describe('strings', () => {
  it('has no empty values', () => {
    for (const [key, value] of Object.entries(strings)) {
      expect(value.trim(), key).not.toBe('');
    }
  });
});
