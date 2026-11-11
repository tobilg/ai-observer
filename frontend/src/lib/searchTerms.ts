// Search terms as the API reads them (storage.SearchTerms): words, and
// "quoted phrases" kept whole; case-insensitive duplicates once. Used to
// highlight what a session search matched (HighlightedText).
export function searchTerms(query: string): string[] {
  const terms: string[] = []
  const seen = new Set<string>()
  const add = (raw: string) => {
    const term = raw.trim()
    const key = term.toLowerCase()
    if (term === '' || seen.has(key)) {
      return
    }
    seen.add(key)
    terms.push(term)
  }

  let term = ''
  let quoted = false
  for (const char of query) {
    if (char === '"') {
      add(term)
      term = ''
      quoted = !quoted
    } else if (/\s/.test(char) && !quoted) {
      add(term)
      term = ''
    } else {
      term += char
    }
  }
  add(term)
  return terms
}
