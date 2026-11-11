import type { TranscriptMessage } from '@/types/sessions'

// Props of the transcript views (ChatBubbleView, TimelineView)
export interface TranscriptViewProps {
  messages: TranscriptMessage[]
  // The position of each message in the whole transcript (default: its
  // index), which keys the rows and their state across filter changes
  positions?: number[]
  // Search terms to mark
  highlight?: readonly string[]
  // The position of the current search match: marked, with its tool
  // content expanded
  activePosition?: number
  // Scrolls to activePosition whenever it changes (per search navigation)
  revealKey?: string
  className?: string
}
