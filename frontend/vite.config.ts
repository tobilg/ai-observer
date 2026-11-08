import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'
import { viteSingleFile } from 'vite-plugin-singlefile'
import path from 'path'

const resolve = {
  alias: {
    '@': path.resolve(__dirname, './src'),
  },
}

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  // The MCP tools' view (MCP Apps, `vite build --mode mcp-app`): one page
  // with scripts and styles inlined, since hosts render it in a sandbox
  // without network access. The backend embeds it (internal/frontend).
  if (mode === 'mcp-app') {
    return {
      plugins: [react(), tailwindcss(), viteSingleFile()],
      // Nothing besides the page: public/ holds the dashboard's files
      publicDir: false,
      build: {
        outDir: '../backend/internal/frontend/mcpapp',
        emptyOutDir: true,
        rolldownOptions: { input: path.resolve(__dirname, 'mcp-app.html') },
      },
      resolve,
    }
  }

  return {
    plugins: [react(), tailwindcss()],
    build: {
      outDir: '../backend/internal/frontend/dist',
      emptyOutDir: true,
    },
    resolve,
    server: {
      port: 5173,
      proxy: {
        '/api': {
          target: 'http://localhost:8080',
          changeOrigin: true,
        },
        '/ws': {
          target: 'ws://localhost:8080',
          ws: true,
        },
      },
    },
  }
})
