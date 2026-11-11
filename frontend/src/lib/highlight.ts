// Highlighting of search terms in text: transcript search (server-side
// terms) and the trace span filter.

// The class of <mark> elements of highlighted terms
export const HIGHLIGHT_CLASS = 'rounded-sm bg-warning/40 text-foreground'

export interface TextPart {
  text: string
  // Whether the part is one of the terms
  match: boolean
}

function escapeRegExp(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

/**
 * A case-insensitive pattern matching any of terms, longest first (so a
 * term that contains another is marked whole); null without terms
 */
export function termsPattern(terms: readonly string[]): RegExp | null {
  const unique = [...new Set(terms.map((t) => t.trim()).filter(Boolean))]
  if (unique.length === 0) {
    return null
  }
  unique.sort((a, b) => b.length - a.length)
  return new RegExp(unique.map(escapeRegExp).join('|'), 'gi')
}

/** Splits text into the parts that are terms and those between them */
export function splitByTerms(text: string, terms: readonly string[]): TextPart[] {
  const pattern = termsPattern(terms)
  if (!pattern || !text) {
    return [{ text, match: false }]
  }
  const parts: TextPart[] = []
  let last = 0
  for (const found of text.matchAll(pattern)) {
    const start = found.index ?? 0
    if (start > last) {
      parts.push({ text: text.slice(last, start), match: false })
    }
    parts.push({ text: found[0], match: true })
    last = start + found[0].length
  }
  if (last < text.length) {
    parts.push({ text: text.slice(last), match: false })
  }
  return parts.length > 0 ? parts : [{ text, match: false }]
}

/** Whether text contains one of terms (case-insensitive) */
export function containsTerm(text: string, terms: readonly string[]): boolean {
  const pattern = termsPattern(terms)
  return pattern !== null && pattern.test(text)
}
