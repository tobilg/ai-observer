import { memo } from 'react'
import { HIGHLIGHT_CLASS, splitByTerms } from '@/lib/highlight'

interface HighlightedTextProps {
  text: string
  // Terms to mark (case-insensitive); none renders the text as it is
  terms?: readonly string[]
}

// Text with the occurrences of terms wrapped in <mark>
export const HighlightedText = memo(function HighlightedText({ text, terms }: HighlightedTextProps) {
  if (!terms || terms.length === 0) {
    return <>{text}</>
  }
  return (
    <>
      {splitByTerms(text, terms).map((part, index) =>
        part.match ? (
          <mark key={index} className={HIGHLIGHT_CLASS}>
            {part.text}
          </mark>
        ) : (
          part.text
        ),
      )}
    </>
  )
})
