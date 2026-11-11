import { useEffect } from 'react'

export const APP_TITLE = 'AI Observer'

// Sets the browser tab's title to "<title> · AI Observer" while the page is
// shown; without a title, "AI Observer"
export function useDocumentTitle(title?: string) {
  useEffect(() => {
    document.title = title ? `${title} · ${APP_TITLE}` : APP_TITLE
    return () => {
      document.title = APP_TITLE
    }
  }, [title])
}
