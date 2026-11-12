import type { ErrorCode } from './apiError'
import { strings } from '../strings'

const byCode: Record<ErrorCode, string> = {
  'not-found': strings.errorNotFound,
  missing: strings.errorMissing,
  'too-large': strings.errorTooLarge,
  'invalid-path': strings.errorInvalidPath,
  exists: strings.errorExists,
  'invalid-name': strings.errorInvalidName,
  'unsupported-kind': strings.errorUnsupportedKind,
  // Not something the user can fix or act on: the general message is the honest one.
  'not-document': strings.errorGeneric,
  'not-a-directory': strings.errorGeneric,
  canceled: strings.errorGeneric,
  'not-running': strings.errorNotRunning,
  'nvim-missing': strings.errorNvimMissing,
  'unnamed-buffer': strings.errorUnnamedBuffer,
  'unsaved-changes': strings.errorUnsavedChanges,
  'pandoc-missing': strings.errorPandocMissing,
  'convert-failed': strings.errorConvertFailed,
  'not-word': strings.errorNotWord,
  'document-too-large': strings.errorDocumentTooLarge,
  'read-only': strings.errorReadOnly,
  conflict: strings.errorConflict,
  'damaged-work': strings.errorDamagedWork,
  'preview-too-large': strings.errorPreviewTooLarge,
  'invalid-format': strings.errorInvalidFormat,
  internal: strings.errorGeneric,
}

/** The Croatian text shown to the user for a stable service error code. */
export function errorMessage(code: ErrorCode): string {
  return byCode[code] ?? strings.errorGeneric
}
