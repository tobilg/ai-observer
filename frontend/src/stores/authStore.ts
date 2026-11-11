import { create } from 'zustand'

// Whether the API rejected a request for lack of the token
// (AI_OBSERVER_AUTH_TOKEN). The query client sets it from any failed query
// or mutation (lib/queryClient.ts) and clears it when a request succeeds
// again; the layout shows a banner with the sign-in hint while it is set.
interface AuthState {
  authRequired: boolean
  setAuthRequired: (required: boolean) => void
}

export const useAuthStore = create<AuthState>((set) => ({
  authRequired: false,
  setAuthRequired: (required) => set((state) => (state.authRequired === required ? state : { authRequired: required })),
}))
