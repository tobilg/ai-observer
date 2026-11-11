import { describe, it, expect } from 'vitest'
import { containsTerm, splitByTerms, termsPattern } from './highlight'

describe('highlight', () => {
  it('splits text into terms and the text between them, ignoring case', () => {
    expect(splitByTerms('Fix the Login bug, then login again', ['login'])).toEqual([
      { text: 'Fix the ', match: false },
      { text: 'Login', match: true },
      { text: ' bug, then ', match: false },
      { text: 'login', match: true },
      { text: ' again', match: false },
    ])
  })

  it('marks the longest of overlapping terms, and escapes them', () => {
    expect(splitByTerms('a.b and a.bc', ['a.b', 'a.bc']).filter((p) => p.match).map((p) => p.text)).toEqual(['a.b', 'a.bc'])
    expect(splitByTerms('axb', ['a.b'])).toEqual([{ text: 'axb', match: false }])
  })

  it('leaves text without terms as it is', () => {
    expect(splitByTerms('text', [])).toEqual([{ text: 'text', match: false }])
    expect(splitByTerms('text', [' '])).toEqual([{ text: 'text', match: false }])
    expect(termsPattern([])).toBeNull()
    expect(containsTerm('Fix the bug', ['BUG'])).toBe(true)
    expect(containsTerm('Fix the bug', ['nope'])).toBe(false)
  })
})
