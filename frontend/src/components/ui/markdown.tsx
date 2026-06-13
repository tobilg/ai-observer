import ReactMarkdown from 'react-markdown'
import { cn } from '@/lib/utils'
import hljs from 'highlight.js/lib/common'
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

interface MarkdownProps {
  children: string
  className?: string
}

export function Markdown({ children, className }: MarkdownProps) {
  return (
    <div className={cn('prose prose-sm dark:prose-invert max-w-none overflow-hidden', className)}>
      <ReactMarkdown
        components={{
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
          const language = match?.[1]

          let highlighted = ''
          try {
            if (language && hljs.getLanguage(language)) {
              highlighted = hljs.highlight(codeText, { language }).value
            } else {
              highlighted = hljs.highlightAuto(codeText).value
            }
          } catch {
            highlighted = escapeHTML(codeText)
          }

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
      }}
      >
        {children}
      </ReactMarkdown>
    </div>
  )
}
