import type { ReactElement, ReactNode } from 'react'
import { render, renderHook, type RenderOptions } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter, Route, Routes } from 'react-router-dom'

// Test helpers for components and hooks that use TanStack Query and the
// router. Each call gets a fresh QueryClient without retries, so tests do not
// share cached data and failures show at once.

export function createTestQueryClient(): QueryClient {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity, refetchOnWindowFocus: false },
      mutations: { retry: false },
    },
  })
}

export interface ProviderOptions {
  // The URL the MemoryRouter starts at, e.g. '/logs?service=claude-code'
  route?: string
  // A route pattern to render the element at, so useParams works, e.g.
  // '/sessions/:sessionId' (route must match it)
  path?: string
  queryClient?: QueryClient
}

function makeWrapper({ route = '/', path, queryClient }: ProviderOptions & { queryClient: QueryClient }) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>
        <MemoryRouter initialEntries={[route]}>
          {path ? (
            <Routes>
              <Route path={path} element={children} />
            </Routes>
          ) : (
            children
          )}
        </MemoryRouter>
      </QueryClientProvider>
    )
  }
}

/**
 * render() inside a QueryClientProvider and a MemoryRouter:
 *
 *   const { queryClient } = renderWithProviders(<LogsPage />, { route: '/logs?severity=ERROR' })
 *   renderWithProviders(<SessionTranscriptPage />, { route: '/sessions/s1', path: '/sessions/:sessionId' })
 */
export function renderWithProviders(
  ui: ReactElement,
  { route, path, queryClient = createTestQueryClient(), ...options }: ProviderOptions & Omit<RenderOptions, 'wrapper'> = {},
) {
  return { queryClient, ...render(ui, { wrapper: makeWrapper({ route, path, queryClient }), ...options }) }
}

/** renderHook() with the same providers as renderWithProviders */
export function renderHookWithProviders<Result, Props>(
  hook: (props: Props) => Result,
  { route, path, queryClient = createTestQueryClient(), initialProps }: ProviderOptions & { initialProps?: Props } = {},
) {
  return { queryClient, ...renderHook(hook, { wrapper: makeWrapper({ route, path, queryClient }), initialProps }) }
}
