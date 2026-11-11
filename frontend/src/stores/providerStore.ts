import { create } from 'zustand'
import type { Provider } from '@/lib/api'

// The supported tools come from the backend's provider table (GET
// /api/providers), so a provider added there is named correctly in the
// dashboard. The providers query (useProviders in lib/queries.ts, mounted by
// the layout) feeds this store; getServiceDisplayName reads it from
// non-React code too, and uses its fallback until the providers have loaded.
// React components re-render on changes through useServiceNames.
//
// The store has no dependency on the API client or TanStack Query, so the
// MCP Apps view, which uses getServiceDisplayName, does not bundle them.
interface ProviderState {
  // Display names by OTLP service name
  displayNames: Record<string, string>
  // The service names of the tools that send several (Codex), by tool name
  toolServices: Record<string, string[]>
  loaded: boolean
  setProviders: (providers: Provider[]) => void
}

export const useProviderStore = create<ProviderState>((set) => ({
  displayNames: {},
  toolServices: {},
  loaded: false,
  setProviders: (providers) => {
    const displayNames: Record<string, string> = {}
    const toolServices: Record<string, string[]> = {}
    for (const provider of providers) {
      displayNames[provider.serviceName] = provider.displayName
      for (const service of provider.otherServices ?? []) {
        displayNames[service.serviceName] = service.displayName
      }
      if (provider.otherServices?.length) {
        toolServices[provider.tool] = [provider.serviceName, ...provider.otherServices.map((s) => s.serviceName)]
      }
    }
    set({ displayNames, toolServices, loaded: true })
  },
}))
