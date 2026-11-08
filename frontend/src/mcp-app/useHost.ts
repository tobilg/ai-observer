import { useCallback, useEffect, useRef, useState } from 'react'
import { App, type McpUiHostContext } from '@modelcontextprotocol/ext-apps'
import { errorOf, structuredOf, type ToolResult } from './data'
import type { CallTool } from './types'

export interface HostState {
  status: 'connecting' | 'connected' | 'failed'
  error?: string
  // The tool whose result the host shows, when the host tells it
  toolName?: string
  args: Record<string, unknown>
  result?: ToolResult
  // Counts the results the host pushed, so views reset their own state
  version: number
}

// The host's theme, or the system's when the host names none
function applyTheme(theme: McpUiHostContext['theme']) {
  const dark = theme ? theme === 'dark' : window.matchMedia?.('(prefers-color-scheme: dark)').matches === true
  document.documentElement.classList.toggle('dark', dark)
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light'
}

// Connects to the MCP Apps host (postMessage to the parent window), keeps
// the latest tool arguments and result it pushes, and calls tools through it
export function useHost(): HostState & { callTool: CallTool } {
  const appRef = useRef<App | null>(null)
  const [state, setState] = useState<HostState>({ status: 'connecting', args: {}, version: 0 })

  useEffect(() => {
    const app = new App({ name: 'AI Observer', version: '1.0.0' }, {}, { autoResize: true })
    appRef.current = app
    // Handlers before connecting, so no notification is missed
    app.ontoolinput = (params) => setState((s) => ({ ...s, args: params.arguments ?? {} }))
    app.ontoolresult = (result) => setState((s) => ({ ...s, result, version: s.version + 1 }))
    app.onhostcontextchanged = (context) => {
      if (context.theme) applyTheme(context.theme)
    }
    app.onteardown = () => ({})
    applyTheme(undefined)

    app
      .connect()
      .then(() => {
        const context = app.getHostContext()
        applyTheme(context?.theme)
        setState((s) => ({ ...s, status: 'connected', toolName: context?.toolInfo?.tool.name }))
      })
      .catch((err: unknown) => {
        setState((s) => ({ ...s, status: 'failed', error: err instanceof Error ? err.message : String(err) }))
      })
    return () => {
      appRef.current = null
      void app.close()
    }
  }, [])

  const callTool = useCallback<CallTool>(async (name, args) => {
    const app = appRef.current
    if (!app) {
      throw new Error('Not connected to the host')
    }
    const result = await app.callServerTool({ name, arguments: args })
    const error = errorOf(result)
    if (error) {
      throw new Error(error)
    }
    return structuredOf(result)
  }, [])

  return { ...state, callTool }
}
