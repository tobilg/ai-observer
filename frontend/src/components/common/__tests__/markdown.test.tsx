import { describe, it, expect, vi } from 'vitest'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Markdown } from '../markdown'

describe('Markdown', () => {
  it('renders markdown and highlights code blocks', () => {
    const { container } = render(<Markdown>{'Some **bold** text\n\n```go\nfunc main() {}\n```'}</Markdown>)
    expect(screen.getByText('bold').tagName).toBe('STRONG')
    expect(container.querySelector('code.hljs')).not.toBeNull()
  })

  it('copies the raw text of a code block', async () => {
    const user = userEvent.setup()
    const writeText = vi.spyOn(navigator.clipboard, 'writeText').mockResolvedValue()
    render(<Markdown>{'```ts\nconst a = 1\nconst b = 2\n```'}</Markdown>)
    await user.click(screen.getByRole('button', { name: 'Copy code' }))
    expect(writeText).toHaveBeenCalledWith('const a = 1\nconst b = 2')
  })

  it('marks search terms in the text, but not in code blocks', () => {
    const { container } = render(
      <Markdown highlight={['login']}>{'The **Login** form and `login()`\n\n```\nlogin()\n```'}</Markdown>,
    )
    const marks = Array.from(container.querySelectorAll('mark')).map((m) => m.textContent)
    expect(marks).toEqual(['Login', 'login'])
    expect(container.querySelector('pre mark')).toBeNull()
  })

  it('renders the same content again from its cache, with or without terms', () => {
    const content = 'Cached *content*'
    const first = render(<Markdown>{content}</Markdown>)
    first.unmount()
    render(<Markdown>{content}</Markdown>)
    expect(screen.getByText('content').tagName).toBe('EM')
    render(<Markdown highlight={['cached']}>{content}</Markdown>)
    expect(screen.getByText('Cached', { selector: 'mark' })).toBeInTheDocument()
  })
})
