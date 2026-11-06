import { lazy, Suspense, type ReactNode } from 'react'
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ThemeProvider } from '@/components/theme-provider'
import { SidebarProvider } from '@/components/ui/sidebar'
import { Toaster } from '@/components/ui/sonner'
import { Skeleton } from '@/components/ui/skeleton'
import { Layout } from '@/components/layout/Layout'
import { ErrorBoundary } from '@/components/ErrorBoundary'

// Pages are loaded on demand so the initial bundle only contains the app shell
const Dashboard = lazy(() => import('@/pages/Dashboard').then((m) => ({ default: m.Dashboard })))
const TracesPage = lazy(() => import('@/pages/TracesPage').then((m) => ({ default: m.TracesPage })))
const TraceDetailPage = lazy(() => import('@/pages/TraceDetailPage').then((m) => ({ default: m.TraceDetailPage })))
const MetricsPage = lazy(() => import('@/pages/MetricsPage').then((m) => ({ default: m.MetricsPage })))
const LogsPage = lazy(() => import('@/pages/LogsPage').then((m) => ({ default: m.LogsPage })))
const SessionsPage = lazy(() => import('@/pages/SessionsPage').then((m) => ({ default: m.SessionsPage })))
const SessionTranscriptPage = lazy(() =>
  import('@/pages/SessionTranscriptPage').then((m) => ({ default: m.SessionTranscriptPage }))
)
const DocsPage = lazy(() => import('@/pages/DocsPage').then((m) => ({ default: m.DocsPage })))

function PageFallback() {
  return (
    <div className="space-y-4 p-6" aria-busy="true">
      <Skeleton className="h-8 w-48" />
      <Skeleton className="h-64 w-full" />
    </div>
  )
}

function page(element: ReactNode) {
  return (
    <ErrorBoundary>
      <Suspense fallback={<PageFallback />}>{element}</Suspense>
    </ErrorBoundary>
  )
}

function App() {
  return (
    <ThemeProvider defaultTheme="system" storageKey="ai-observer-theme">
      <SidebarProvider>
        <BrowserRouter>
          <Routes>
            <Route path="/" element={<Layout />}>
              <Route index element={page(<Dashboard />)} />
              <Route path="dashboard/:id" element={page(<Dashboard />)} />
              <Route path="traces" element={page(<TracesPage />)} />
              <Route path="traces/:kind/:id" element={page(<TraceDetailPage />)} />
              <Route path="metrics" element={page(<MetricsPage />)} />
              <Route path="logs" element={page(<LogsPage />)} />
              <Route path="sessions" element={page(<SessionsPage />)} />
              <Route path="sessions/:sessionId" element={page(<SessionTranscriptPage />)} />
              <Route path="docs" element={page(<DocsPage />)} />
            </Route>
          </Routes>
        </BrowserRouter>
        <Toaster />
      </SidebarProvider>
    </ThemeProvider>
  )
}

export default App
