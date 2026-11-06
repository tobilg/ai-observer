import { create } from 'zustand'
import { api } from '@/lib/api'

// The supported tools come from the backend's provider table (GET
// /api/providers), so a provider added there is named correctly in the
// dashboard. Until they have loaded, getServiceDisplayName uses its fallback.
interface ProviderState {
  // Display names by OTLP service name
  displayNames: Record<string, string>
  loaded: boolean
  load: () => Promise<void>
}

export const useProviderStore = create<ProviderState>((set, get) => ({
  displayNames: {},
  loaded: false,
  load: async () => {
    if (get().loaded) {
      return
    }
    try {
      const { providers } = await api.getProviders()
      const displayNames: Record<string, string> = {}
      for (const provider of providers ?? []) {
        displayNames[provider.serviceName] = provider.displayName
      }
      set({ displayNames, loaded: true })
    } catch {
      // Keep the fallback names; the next page load tries again
    }
  },
}))
