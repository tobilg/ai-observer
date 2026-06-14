import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { ThemeProvider } from '@/components/theme-provider'
import { SidebarProvider } from '@/components/ui/sidebar'
import { Toaster } from '@/components/ui/sonner'
import { Layout } from '@/components/layout/Layout'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import { Dashboard } from '@/pages/Dashboard'
import { TracesPage } from '@/pages/TracesPage'
import { TraceDetailPage } from '@/pages/TraceDetailPage'
import { MetricsPage } from '@/pages/MetricsPage'
import { LogsPage } from '@/pages/LogsPage'
import { SessionsPage } from '@/pages/SessionsPage'
import { SessionTranscriptPage } from '@/pages/SessionTranscriptPage'
import { DocsPage } from '@/pages/DocsPage'

function App() {
  return (
    <ThemeProvider defaultTheme="system" storageKey="ai-observer-theme">
      <SidebarProvider>
        <BrowserRouter>
          <Routes>
            <Route path="/" element={<Layout />}>
              <Route index element={
                <ErrorBoundary>
                  <Dashboard />
                </ErrorBoundary>
              } />
              <Route path="dashboard/:id" element={
                <ErrorBoundary>
                  <Dashboard />
                </ErrorBoundary>
              } />
              <Route path="traces" element={
                <ErrorBoundary>
                  <TracesPage />
                </ErrorBoundary>
              } />
              <Route path="traces/:kind/:id" element={
                <ErrorBoundary>
                  <TraceDetailPage />
                </ErrorBoundary>
              } />
              <Route path="metrics" element={
                <ErrorBoundary>
                  <MetricsPage />
                </ErrorBoundary>
              } />
              <Route path="logs" element={
                <ErrorBoundary>
                  <LogsPage />
                </ErrorBoundary>
              } />
              <Route path="sessions" element={
                <ErrorBoundary>
                  <SessionsPage />
                </ErrorBoundary>
              } />
              <Route path="sessions/:sessionId" element={
                <ErrorBoundary>
                  <SessionTranscriptPage />
                </ErrorBoundary>
              } />
              <Route path="docs" element={
                <ErrorBoundary>
                  <DocsPage />
                </ErrorBoundary>
              } />
            </Route>
          </Routes>
        </BrowserRouter>
        <Toaster />
      </SidebarProvider>
    </ThemeProvider>
  )
}

export default App
