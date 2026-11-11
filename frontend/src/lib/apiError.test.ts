import { describe, expect, it } from 'vitest';
import { errorCode } from './apiError';

// The shape Wails delivers: a RuntimeError (name "RuntimeError") with message = Go error text and
// cause = parsed JSON from MarshalError (runtime.js, runtimeCallWithID).
function wailsError(message: string, cause?: unknown): Error {
  const err = new Error(message, cause === undefined ? undefined : { cause });
  err.name = 'RuntimeError';
  return err;
}

describe('errorCode', () => {
  it.each([
    ['cause code', wailsError('exists: x', { code: 'exists' }), 'exists'],
    ['cause wins over text', wailsError('internal: x', { code: 'not-found' }), 'not-found'],
    ['text prefix only', wailsError('invalid-name: bad'), 'invalid-name'],
    ['plain string', 'too-large: tree', 'too-large'],
    ['editor not running', wailsError('not-running: x', { code: 'not-running' }), 'not-running'],
    ['editor missing', wailsError('nvim-missing: x', { code: 'nvim-missing' }), 'nvim-missing'],
    ['unnamed buffer', wailsError('unnamed-buffer: x'), 'unnamed-buffer'],
    ['unsaved changes', wailsError('unsaved-changes: x', { code: 'unsaved-changes' }), 'unsaved-changes'],
    ['pandoc missing', wailsError('pandoc-missing: x', { code: 'pandoc-missing' }), 'pandoc-missing'],
    ['convert failed', wailsError('convert-failed: x', { code: 'convert-failed' }), 'convert-failed'],
    ['not a Word document', wailsError('not-word: x'), 'not-word'],
    ['document too large', wailsError('document-too-large: x', { code: 'document-too-large' }), 'document-too-large'],
    ['read only', wailsError('read-only: x', { code: 'read-only' }), 'read-only'],
    ['conflict', wailsError('conflict: x', { code: 'conflict' }), 'conflict'],
    ['damaged work', wailsError('damaged-work: x'), 'damaged-work'],
    ['unknown cause code, known text', wailsError('missing: x', { code: 'bogus' }), 'missing'],
    ['unknown code', wailsError('x', { code: 'bogus' }), 'internal'],
    ['text without code', wailsError('something broke'), 'internal'],
    ['unknown prefix', wailsError('network: down'), 'internal'],
    ['string without code', 'boom', 'internal'],
    ['undefined', undefined, 'internal'],
    ['null', null, 'internal'],
    ['object without message', {}, 'internal'],
    ['non-string message', { message: 5 }, 'internal'],
    ['null cause', wailsError('nope', null), 'internal'],
  ])('%s', (_name, input, want) => {
    expect(errorCode(input)).toBe(want);
  });
});
