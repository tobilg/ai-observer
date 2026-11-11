import type { ReactNode } from 'react'
import { useLocation } from 'react-router-dom'
import { ErrorBoundary } from '@/components/ErrorBoundary'

// The error boundary of a page: navigating to another path clears an error,
// although React keeps the boundary mounted (every route renders it at the
// same position)
export function RouteErrorBoundary({ children }: { children: ReactNode }) {
  const { pathname } = useLocation()
  return <ErrorBoundary resetKeys={[pathname]}>{children}</ErrorBoundary>
}
