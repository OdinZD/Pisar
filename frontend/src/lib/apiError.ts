// Stable error codes of the Go services (see apiError in projectservice.go).
//
// How a Go error reaches JavaScript (Wails v3.1.0; pkg/application/bindings.go, transport_http.go and
// @wailsio/runtime/dist/runtime.js): a bound method that returns an error rejects its Promise with
// a RuntimeError whose `message` is the Go error text and whose `cause` is the JSON produced by the
// service's MarshalError. Our service sends {"code": "<code>"} as the cause, and its error text also
// starts with "<code>: ". The cause is read first; the text prefix is the fallback.
export const errorCodes = [
  'not-found',
  'missing',
  'too-large',
  'invalid-path',
  'exists',
  'invalid-name',
  'unsupported-kind',
  'not-document',
  'not-a-directory',
  'canceled',
  'internal',
] as const;

export type ErrorCode = (typeof errorCodes)[number];

function asCode(value: unknown): ErrorCode | undefined {
  return errorCodes.find((c) => c === value);
}

function fromMessage(message: unknown): ErrorCode | undefined {
  if (typeof message !== 'string') return undefined;
  const i = message.indexOf(': ');
  return i < 0 ? undefined : asCode(message.slice(0, i));
}

/** Returns the stable code of an error thrown by a bound Go method; 'internal' for anything else. */
export function errorCode(err: unknown): ErrorCode {
  if (typeof err === 'string') return fromMessage(err) ?? 'internal';
  if (err === null || typeof err !== 'object') return 'internal';
  const { cause, message } = err as { cause?: unknown; message?: unknown };
  if (cause !== null && typeof cause === 'object') {
    const code = asCode((cause as { code?: unknown }).code);
    if (code) return code;
  }
  return fromMessage(message) ?? 'internal';
}
