import { create } from 'zustand'
import { api } from '@/lib/api'

// The supported tools come from the backend's provider table (GET
// /api/providers), so a provider added there is named correctly in the
// dashboard. Until they have loaded, getServiceDisplayName uses its fallback.
interface ProviderState {
  // Display names by OTLP service name
  displayNames: Record<string, string>
  // The service names of the tools that send several (Codex), by tool name
  toolServices: Record<string, string[]>
  loaded: boolean
  load: () => Promise<void>
}

export const useProviderStore = create<ProviderState>((set, get) => ({
  displayNames: {},
  toolServices: {},
  loaded: false,
  load: async () => {
    if (get().loaded) {
      return
    }
    try {
      const { providers } = await api.getProviders()
      const displayNames: Record<string, string> = {}
      const toolServices: Record<string, string[]> = {}
      for (const provider of providers ?? []) {
        displayNames[provider.serviceName] = provider.displayName
        for (const service of provider.otherServices ?? []) {
          displayNames[service.serviceName] = service.displayName
        }
        if (provider.otherServices?.length) {
          toolServices[provider.tool] = [provider.serviceName, ...provider.otherServices.map((s) => s.serviceName)]
        }
      }
      set({ displayNames, toolServices, loaded: true })
    } catch {
      // Keep the fallback names; the next page load tries again
    }
  },
}))
