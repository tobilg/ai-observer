import { MutationCache, QueryCache, QueryClient } from '@tanstack/react-query'
import { isAuthError, isRetryableError } from '@/lib/errors'
import { useAuthStore } from '@/stores/authStore'

// The only retry layer of the dashboard (lib/api.ts does not retry): reads
// that failed with a network error, 429 or 5xx other than 503/504 are tried
// again up to MAX_RETRIES times with exponential backoff. 4xx, 503, 504,
// client timeouts and aborts fail at once. Mutations are never retried.
export const MAX_RETRIES = 3
const RETRY_BASE_DELAY_MS = 500
const RETRY_MAX_DELAY_MS = 4000

// failureCount is the number of failed attempts before this one (0 after the
// first failure), as TanStack Query passes it
export function shouldRetry(failureCount: number, error: unknown): boolean {
  return failureCount < MAX_RETRIES && isRetryableError(error)
}

// 500 ms, 1 s, 2 s, ... (at most 4 s), with 10% jitter so clients that failed
// together do not retry together
export function retryDelay(failureCount: number): number {
  const delay = Math.min(RETRY_BASE_DELAY_MS * 2 ** failureCount, RETRY_MAX_DELAY_MS)
  return delay + delay * 0.1 * (Math.random() * 2 - 1)
}

function trackAuth(error: unknown) {
  if (isAuthError(error)) {
    useAuthStore.getState().setAuthRequired(true)
  }
}

function clearAuth() {
  if (useAuthStore.getState().authRequired) {
    useAuthStore.getState().setAuthRequired(false)
  }
}

export function createQueryClient(): QueryClient {
  return new QueryClient({
    queryCache: new QueryCache({ onError: trackAuth, onSuccess: clearAuth }),
    mutationCache: new MutationCache({ onError: trackAuth, onSuccess: clearAuth }),
    defaultOptions: {
      queries: {
        retry: shouldRetry,
        retryDelay,
        // Live pages refresh on real-time notifications and when the tab is
        // shown again (useLiveRefresh); a focus refetch would only repeat it
        refetchOnWindowFocus: false,
      },
      mutations: {
        retry: false,
      },
    },
  })
}

// The client of the dashboard app (App.tsx); tests create their own
// (test/queryWrapper.tsx)
export const queryClient = createQueryClient()
