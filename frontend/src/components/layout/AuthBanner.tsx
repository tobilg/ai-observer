import { KeyRound } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { useAuthStore } from '@/stores/authStore'

// Shown while API requests fail with 401 (the server runs with
// AI_OBSERVER_AUTH_TOKEN and the browser has no session cookie yet). It
// cannot be dismissed: nothing loads without the token. It disappears once a
// request succeeds.
export function AuthBanner() {
  const authRequired = useAuthStore((s) => s.authRequired)
  if (!authRequired) {
    return null
  }
  return (
    <div className="px-6 pt-4">
      <Alert variant="destructive">
        <KeyRound />
        <AlertTitle>Authentication required</AlertTitle>
        <AlertDescription>
          <p>
            Open AI Observer once with <code className="font-mono">?token=&lt;AI_OBSERVER_AUTH_TOKEN&gt;</code>, the
            token the server was started with. The dashboard keeps a session cookie afterwards.
          </p>
        </AlertDescription>
      </Alert>
    </div>
  )
}
