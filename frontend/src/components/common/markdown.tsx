import { memo } from 'react'
import ReactMarkdown, { type Components } from 'react-markdown'
import { cn } from '@/lib/utils'
import hljs from 'highlight.js/lib/core'
import bash from 'highlight.js/lib/languages/bash'
import css from 'highlight.js/lib/languages/css'
import diff from 'highlight.js/lib/languages/diff'
import go from 'highlight.js/lib/languages/go'
import ini from 'highlight.js/lib/languages/ini'
import java from 'highlight.js/lib/languages/java'
import javascript from 'highlight.js/lib/languages/javascript'
import json from 'highlight.js/lib/languages/json'
import markdown from 'highlight.js/lib/languages/markdown'
import plaintext from 'highlight.js/lib/languages/plaintext'
import python from 'highlight.js/lib/languages/python'
import rust from 'highlight.js/lib/languages/rust'
import shell from 'highlight.js/lib/languages/shell'
import sql from 'highlight.js/lib/languages/sql'
import typescript from 'highlight.js/lib/languages/typescript'
import xml from 'highlight.js/lib/languages/xml'
import yaml from 'highlight.js/lib/languages/yaml'
import 'highlight.js/styles/github.css'

// Simple HTML escape for fallback when highlight.js fails
function escapeHTML(str: string): string {
  return str
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#039;')
}

// Languages that appear in AI coding sessions. Registering them on the core
// build instead of loading highlight.js's "common" set keeps the transcript
// bundle small; their aliases (ts, js, sh, py, yml, html, toml, ...) are
// registered with them. Other languages are shown as escaped plain text.
const LANGUAGES = {
  bash, css, diff, go, ini, java, javascript, json, markdown, plaintext,
  python, rust, shell, sql, typescript, xml, yaml,
}
for (const [name, language] of Object.entries(LANGUAGES)) {
  hljs.registerLanguage(name, language)
}

// highlightAuto tries every language, which is slow for long tool output
const MAX_AUTO_DETECT_LENGTH = 10_000

// Transcripts re-render often (live updates, expanding messages), so
// highlighted blocks are cached instead of being highlighted on every render
const HIGHLIGHT_CACHE_SIZE = 200
const highlightCache = new Map<string, string>()

function highlightCode(code: string, language?: string): string {
  const key = `${language ?? ''}\u0000${code}`
  const cached = highlightCache.get(key)
  if (cached !== undefined) {
    return cached
  }

  let html: string
  try {
    if (language && hljs.getLanguage(language)) {
      html = hljs.highlight(code, { language }).value
    } else if (code.length <= MAX_AUTO_DETECT_LENGTH) {
      html = hljs.highlightAuto(code).value
    } else {
      html = escapeHTML(code)
    }
  } catch {
    html = escapeHTML(code)
  }

  if (highlightCache.size >= HIGHLIGHT_CACHE_SIZE) {
    const oldest = highlightCache.keys().next().value
    if (oldest !== undefined) {
      highlightCache.delete(oldest)
    }
  }
  highlightCache.set(key, html)
  return html
}

interface MarkdownProps {
  children: string
  className?: string
}

// Defined once, so ReactMarkdown does not get new components on every render
const markdownComponents: Components = {
  // Headings
  h1: ({ children }) => (
    <h1 className="text-xl font-bold mt-4 mb-2 first:mt-0">{children}</h1>
  ),
  h2: ({ children }) => (
    <h2 className="text-lg font-bold mt-3 mb-2 first:mt-0">{children}</h2>
  ),
  h3: ({ children }) => (
    <h3 className="text-base font-semibold mt-3 mb-1 first:mt-0">{children}</h3>
  ),
  h4: ({ children }) => (
    <h4 className="text-sm font-semibold mt-2 mb-1 first:mt-0">{children}</h4>
  ),
  h5: ({ children }) => (
    <h5 className="text-sm font-medium mt-2 mb-1 first:mt-0">{children}</h5>
  ),
  h6: ({ children }) => (
    <h6 className="text-sm font-medium mt-2 mb-1 first:mt-0">{children}</h6>
  ),
  // Paragraphs
  p: ({ children }) => (
    <p className="mb-2 last:mb-0">{children}</p>
  ),
  // Code blocks
  pre: ({ children }) => (
    <pre className="bg-muted rounded-md p-3 overflow-x-auto my-2 text-xs max-w-full">{children}</pre>
  ),
  code: ({ className, children, ...props }) => {
    // Check if this is inline code (no className) or a code block
    const isInline = !className
    if (isInline) {
      return (
        <code className="bg-muted px-1.5 py-0.5 rounded text-xs font-mono" {...props}>
          {children}
        </code>
      )
    }

    const rawCode = Array.isArray(children) ? children.join('') : String(children)
    const codeText = rawCode.replace(/\n$/, '')
    const match = /language-([\w-]+)/.exec(className || '')
    const highlighted = highlightCode(codeText, match?.[1])

    return (
      <code
        className={cn('hljs font-mono text-xs', className)}
        dangerouslySetInnerHTML={{ __html: highlighted }}
        {...props}
      />
    )
  },
  // Links
  a: ({ href, children }) => (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="text-primary underline-offset-2 hover:underline"
    >
      {children}
    </a>
  ),
  // Lists
  ul: ({ children }) => (
    <ul className="list-disc list-inside my-2 space-y-1">{children}</ul>
  ),
  ol: ({ children }) => (
    <ol className="list-decimal list-inside my-2 space-y-1">{children}</ol>
  ),
  li: ({ children }) => (
    <li className="text-sm">{children}</li>
  ),
  // Tables
  table: ({ children }) => (
    <div className="overflow-x-auto max-w-full">
      <table className="min-w-0">{children}</table>
    </div>
  ),
  // Blockquotes
  blockquote: ({ children }) => (
    <blockquote className="border-l-2 border-muted-foreground/30 pl-3 my-2 text-muted-foreground italic">
      {children}
    </blockquote>
  ),
  // Horizontal rule
  hr: () => (
    <hr className="my-4 border-border" />
  ),
  // Strong and emphasis
  strong: ({ children }) => (
    <strong className="font-semibold">{children}</strong>
  ),
  em: ({ children }) => (
    <em className="italic">{children}</em>
  ),
}

export const Markdown = memo(function Markdown({ children, className }: MarkdownProps) {
  return (
    <div className={cn('prose prose-sm dark:prose-invert max-w-none overflow-hidden', className)}>
      <ReactMarkdown components={markdownComponents}>{children}</ReactMarkdown>
    </div>
  )
})
