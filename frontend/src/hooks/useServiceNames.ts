import { useMemo } from 'react'
import { useShallow } from 'zustand/react/shallow'
import { serviceDisplayName, serviceFilterOptionsFor } from '@/lib/metricMetadata'
import { useProviderStore } from '@/stores/providerStore'

/**
 * getServiceDisplayName and serviceFilterOptions for components: they
 * re-render, and the functions change identity, when the providers have
 * loaded (useProviders, mounted by the layout), so names from the backend's
 * provider table replace the fallback names.
 *
 *   const { displayName, filterOptions } = useServiceNames()
 *   displayName(trace.serviceName)
 *   filterOptions(services).map((o) => <NativeSelectOption key={o.value} value={o.value}>{o.label}</NativeSelectOption>)
 */
export function useServiceNames() {
  const names = useProviderStore(
    useShallow((s) => ({ displayNames: s.displayNames, toolServices: s.toolServices, loaded: s.loaded })),
  )
  return useMemo(
    () => ({
      displayName: (serviceName: string) => serviceDisplayName(serviceName, names),
      filterOptions: (services: string[]) => serviceFilterOptionsFor(services, names),
      loaded: names.loaded,
    }),
    [names],
  )
}
