import { forwardRef } from 'react'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import type { DocSection as DocSectionType, DocContent } from '@/data/documentation'

interface DocSectionProps {
  section: DocSectionType
}

function renderContent(content: DocContent, index: number) {
  switch (content.type) {
    case 'paragraph':
      return (
        <p
          key={index}
          className="text-base text-muted-foreground leading-relaxed mb-4"
        >
          {content.text}
        </p>
      )

    case 'heading':
      if (content.level === 3) {
        return (
          <h3
            key={index}
            className="text-lg font-semibold mt-6 mb-3 scroll-mt-20"
          >
            {content.text}
          </h3>
        )
      }
      return (
        <h2
          key={index}
          className="text-2xl font-bold tracking-tight mt-8 mb-4 scroll-mt-20"
        >
          {content.text}
        </h2>
      )

    case 'list':
      return (
        <ul
          key={index}
          className="list-disc list-outside ml-6 space-y-2 mb-4 text-muted-foreground"
        >
          {content.items?.map((item, i) => (
            <li key={i} className="leading-relaxed">
              {item}
            </li>
          ))}
        </ul>
      )

    case 'note':
      return (
        <p key={index} className="font-medium text-sm mt-4 mb-2">
          {content.text}
        </p>
      )

    case 'table':
      if (!content.table) return null
      return (
        <div key={index} className="mb-6 rounded-md border overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                {content.table.headers.map((header, i) => (
                  <TableHead key={i} className="whitespace-nowrap">
                    {header}
                  </TableHead>
                ))}
              </TableRow>
            </TableHeader>
            <TableBody>
              {content.table.rows.map((row, rowIndex) => (
                <TableRow key={rowIndex}>
                  {row.cells.map((cell, cellIndex) => (
                    <TableCell
                      key={cellIndex}
                      // Descriptions wrap instead of widening the table
                      className={cellIndex === 0 ? 'font-mono text-xs' : 'whitespace-normal min-w-48'}
                    >
                      {cell}
                    </TableCell>
                  ))}
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )

    case 'code':
      return (
        <figure key={index} className="mb-4">
          {content.text && (
            <figcaption className="text-sm font-medium mb-1.5">{content.text}</figcaption>
          )}
          <pre className="bg-muted rounded-md border p-3 overflow-x-auto text-xs leading-relaxed">
            <code>{content.code}</code>
          </pre>
        </figure>
      )

    default:
      return null
  }
}

export const DocSection = forwardRef<HTMLElement, DocSectionProps>(
  function DocSection({ section }, ref) {
    return (
      <section ref={ref} id={section.id} className="mb-12">
        <h2 className="text-2xl font-bold tracking-tight mb-4 scroll-mt-20">
          {section.title}
        </h2>
        {section.content.map((content, index) => renderContent(content, index))}
      </section>
    )
  }
)
