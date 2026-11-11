import { describe, expect, it } from 'vitest'
import { searchTerms } from './searchTerms'

describe('searchTerms', () => {
  it('splits words and keeps quoted phrases whole', () => {
    expect(searchTerms('fix  "auth token" bug')).toEqual(['fix', 'auth token', 'bug'])
  })

  it('drops empty terms and case-insensitive repeats', () => {
    expect(searchTerms('Bug bug "" "  "')).toEqual(['Bug'])
    expect(searchTerms('   ')).toEqual([])
  })

  it('reads an unclosed quote to the end', () => {
    expect(searchTerms('a "b c')).toEqual(['a', 'b c'])
  })
})
