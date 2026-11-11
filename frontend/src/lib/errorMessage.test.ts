import { describe, expect, it } from 'vitest'
import { errorCodes } from './apiError'
import { errorMessage } from './errorMessage'
import { strings } from '../strings'

describe('errorMessage', () => {
  it.each(errorCodes)('has a non-empty text for %s', (code) => {
    expect(errorMessage(code).trim()).not.toBe('')
  })

  it('uses specific texts for the codes a user can fix', () => {
    expect(errorMessage('exists')).toBe(strings.errorExists)
    expect(errorMessage('invalid-name')).toBe(strings.errorInvalidName)
    expect(errorMessage('internal')).toBe(strings.errorGeneric)
    expect(errorMessage('nvim-missing')).toContain('brew install neovim')
    expect(errorMessage('not-running')).toBe(strings.errorNotRunning)
    expect(errorMessage('unnamed-buffer')).toBe(strings.errorUnnamedBuffer)
    expect(errorMessage('unsaved-changes')).toBe(strings.errorUnsavedChanges)
    expect(errorMessage('unsaved-changes')).toContain('⌘S')
  })
})
