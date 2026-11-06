import { useSyncExternalStore } from 'react'

function getSnapshot() {
  return document.visibilityState !== 'hidden'
}

function getServerSnapshot() {
  return true
}

function subscribe(callback: () => void) {
  document.addEventListener('visibilitychange', callback)
  return () => document.removeEventListener('visibilitychange', callback)
}

// usePageVisible reports whether the page is visible. Polling pauses while the
// tab is hidden, and effects that depend on it refresh when it is shown again.
export function usePageVisible() {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)
}
