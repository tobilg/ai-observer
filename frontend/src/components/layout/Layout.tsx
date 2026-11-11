import { Outlet } from 'react-router-dom'
import { Header } from './Header'
import { AppSidebar } from './Sidebar'
import { AuthBanner } from './AuthBanner'
import { SidebarInset } from '@/components/ui/sidebar'
import { useWebSocket } from '@/hooks/useWebSocket'
import { useProviders } from '@/lib/queries'

export function Layout() {
  const { isConnected } = useWebSocket()
  // Loads the tools' display names once for all pages (useServiceNames)
  useProviders()

  return (
    <>
      <AppSidebar />
      <SidebarInset className="min-w-0">
        <Header isConnected={isConnected} />
        <AuthBanner />
        <main className="px-6 py-6 min-w-0 overflow-x-hidden">
          <Outlet />
        </main>
      </SidebarInset>
    </>
  )
}
