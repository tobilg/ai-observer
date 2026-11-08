import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import '@/index.css'
import { ErrorBoundary } from '@/components/ErrorBoundary'
import { McpApp } from './McpApp'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ErrorBoundary>
      <McpApp />
    </ErrorBoundary>
  </StrictMode>,
)
