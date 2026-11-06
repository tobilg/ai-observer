import { Outlet } from 'react-router-dom'
import { Header } from './Header'
import { AppSidebar } from './Sidebar'
import { SidebarInset } from '@/components/ui/sidebar'
import { useEffect } from 'react'
import { useWebSocket } from '@/hooks/useWebSocket'
import { useProviderStore } from '@/stores/providerStore'

export function Layout() {
  const { isConnected } = useWebSocket()
  const loadProviders = useProviderStore((s) => s.load)
  // Pages read service names through getServiceDisplayName; subscribing here
  // re-renders them once the provider list has loaded
  useProviderStore((s) => s.displayNames)

  useEffect(() => {
    loadProviders()
  }, [loadProviders])

  return (
    <>
      <AppSidebar />
      <SidebarInset className="min-w-0">
        <Header isConnected={isConnected} />
        <main className="px-6 py-6 min-w-0 overflow-x-hidden">
          <Outlet />
        </main>
      </SidebarInset>
    </>
  )
}
