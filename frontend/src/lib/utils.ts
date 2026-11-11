import { type ClassValue, clsx } from 'clsx'
import { twMerge } from 'tailwind-merge'

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// A span duration in nanoseconds (formatDurationNanos of lib/format.ts)
export { formatDurationNanos as formatDuration } from '@/lib/format'

export function formatTimestamp(timestamp: string | Date): string {
  const date = typeof timestamp === 'string' ? new Date(timestamp) : timestamp
  return date.toLocaleString()
}

export function formatRelativeTime(timestamp: string | Date): string {
  const date = typeof timestamp === 'string' ? new Date(timestamp) : timestamp
  const now = new Date()
  const diff = now.getTime() - date.getTime()

  const seconds = Math.floor(diff / 1000)
  if (seconds < 60) return `${seconds}s ago`

  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes}m ago`

  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours}h ago`

  const days = Math.floor(hours / 24)
  return `${days}d ago`
}

export function getStatusColor(status: string): string {
  switch (status.toUpperCase()) {
    case 'OK':
      return 'bg-success text-white'
    case 'ERROR':
      return 'bg-error text-white'
    default:
      return 'bg-muted text-muted-foreground'
  }
}

export function getSeverityColor(severity: string): string {
  switch (severity.toUpperCase()) {
    case 'FATAL':
    case 'ERROR':
      return 'bg-error text-white'
    case 'WARN':
    case 'WARNING':
      return 'bg-warning text-black'
    case 'INFO':
      return 'bg-info text-white'
    case 'DEBUG':
    case 'TRACE':
      return 'bg-muted text-muted-foreground'
    default:
      return 'bg-secondary text-secondary-foreground'
  }
}

export function getSpanKindIcon(kind: string | undefined): string {
  switch (kind?.toUpperCase()) {
    case 'CLIENT':
      return '→'
    case 'SERVER':
      return '←'
    case 'PRODUCER':
      return '↑'
    case 'CONSUMER':
      return '↓'
    case 'INTERNAL':
    default:
      return '○'
  }
}

export function formatEventTime(eventTimestamp: string | Date, spanTimestamp: string | Date): string {
  const eventTime = typeof eventTimestamp === 'string' ? new Date(eventTimestamp) : eventTimestamp
  const spanTime = typeof spanTimestamp === 'string' ? new Date(spanTimestamp) : spanTimestamp
  const diffMs = eventTime.getTime() - spanTime.getTime()

  if (diffMs < 1000) {
    return `${diffMs}ms`
  }
  return `${(diffMs / 1000).toFixed(2)}s`
}

/**
 * Format a duration in seconds to a human-readable string.
 * E.g., 15 -> "15 seconds", 60 -> "1 minute", 3600 -> "1 hour"
 */
export function formatIntervalSeconds(seconds: number): string {
  if (seconds < 60) return seconds === 1 ? '1 second' : `${seconds} seconds`
  const minutes = seconds / 60
  if (minutes < 60) return minutes === 1 ? '1 minute' : `${minutes} minutes`
  const hours = minutes / 60
  if (hours < 24) return hours === 1 ? '1 hour' : `${hours} hours`
  const days = hours / 24
  return days === 1 ? '1 day' : `${days} days`
}
