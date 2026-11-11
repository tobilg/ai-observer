// Tool output of file reads carries line numbers ("   12→foo"); CodeBlock
// shows them in a column of their own

export type ParsedLine = {
  lineNumber?: string
  content: string
}

export function parseArrowNumberedLines(text: string): { lines: ParsedLine[]; hasLineNumbers: boolean } {
  const rawLines = text.split('\n')
  const parsed: ParsedLine[] = []
  let numberedCount = 0

  // Matches: "   12→foo" or "12 -> foo" (we only strip when a line number is clearly present)
  const re = /^\s*(\d+)\s*(?:→|->)\s?(.*)$/

  for (const line of rawLines) {
    const m = line.match(re)
    if (m) {
      numberedCount++
      parsed.push({ lineNumber: m[1], content: m[2] ?? '' })
    } else {
      parsed.push({ content: line })
    }
  }

  // Only treat as line-numbered output if it’s consistently present (avoid stripping real arrows)
  const hasLineNumbers = parsed.length > 0 && numberedCount >= Math.min(3, parsed.length)

  if (!hasLineNumbers) {
    return { lines: [{ content: text }], hasLineNumbers: false }
  }

  return { lines: parsed, hasLineNumbers: true }
}
