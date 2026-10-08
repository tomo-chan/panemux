import { fireEvent, render, screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { LABEL_SUGGESTION_FOLD, LabelSuggestions } from './LabelSuggestions'

// More labels than the fold shows, in order.
const many = Array.from({ length: LABEL_SUGGESTION_FOLD + 3 }, (_, i) => `label-${String(i).padStart(2, '0')}`)

function tagNames(): string[] {
  return within(screen.getByRole('group', { name: 'Used before:' }))
    .getAllByRole('button')
    .filter((b) => !b.hasAttribute('aria-expanded'))
    .map((b) => b.getAttribute('aria-label') ?? b.textContent ?? '')
}

describe('LabelSuggestions', () => {
  it('renders nothing with no labels and nothing to say', () => {
    const { container } = render(<LabelSuggestions labels={[]} onPick={vi.fn()} />)
    expect(container).toBeEmptyDOMElement()
  })

  it('offers each label as a tag that adds it', () => {
    const onPick = vi.fn()
    render(<LabelSuggestions labels={['bug', 'docs']} onPick={onPick} />)

    expect(tagNames()).toEqual(['Add label bug', 'Add label docs'])
    const bug = screen.getByRole('button', { name: 'Add label bug' })
    expect(bug).toHaveTextContent('+ bug')
    expect(bug).not.toHaveAttribute('aria-pressed')
    fireEvent.click(bug)
    expect(onPick).toHaveBeenCalledWith('bug')
  })

  it('offers toggle tags, pressed for the labels entered', () => {
    const onPick = vi.fn()
    render(<LabelSuggestions labels={['bug', 'docs']} pressed={['docs']} onPick={onPick} />)

    // The ✓ and + marks are what the eye reads; aria-pressed says the same.
    const bug = screen.getByRole('button', { name: 'bug' })
    const docs = screen.getByRole('button', { name: 'docs' })
    expect(bug).toHaveAttribute('aria-pressed', 'false')
    expect(bug).toHaveTextContent('+ bug')
    expect(docs).toHaveAttribute('aria-pressed', 'true')
    expect(docs).toHaveTextContent('✓ docs')
    fireEvent.click(docs)
    expect(onPick).toHaveBeenCalledWith('docs')
  })

  it('offers no fold button at or under the fold', () => {
    render(<LabelSuggestions labels={many.slice(0, LABEL_SUGGESTION_FOLD)} onPick={vi.fn()} />)
    expect(tagNames()).toHaveLength(LABEL_SUGGESTION_FOLD)
    expect(screen.queryByRole('button', { expanded: false })).toBeNull()
    expect(screen.queryByRole('button', { expanded: true })).toBeNull()
  })

  it('folds past the first labels, and expands and folds again', () => {
    render(<LabelSuggestions labels={many} onPick={vi.fn()} />)
    const group = screen.getByRole('group', { name: 'Used before:' })

    expect(tagNames()).toEqual(many.slice(0, LABEL_SUGGESTION_FOLD).map((l) => `Add label ${l}`))
    const more = screen.getByRole('button', { name: '+3 more' })
    expect(more).toHaveAttribute('aria-expanded', 'false')
    expect(more).toHaveAttribute('aria-controls', group.id)

    fireEvent.click(more)
    expect(tagNames()).toEqual(many.map((l) => `Add label ${l}`))
    const less = screen.getByRole('button', { name: 'Show less' })
    expect(less).toHaveAttribute('aria-expanded', 'true')

    fireEvent.click(less)
    expect(tagNames()).toHaveLength(LABEL_SUGGESTION_FOLD)
    expect(screen.getByRole('button', { name: '+3 more' })).toHaveAttribute('aria-expanded', 'false')
  })

  it('never folds a pressed tag away', () => {
    const last = many[many.length - 1]
    render(<LabelSuggestions labels={many} pressed={[last]} onPick={vi.fn()} />)
    expect(screen.getByRole('button', { name: last })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('button', { name: '+2 more' })).toBeInTheDocument()
  })

  it('says why there is nothing to offer', () => {
    render(<LabelSuggestions labels={[]} message="Every label used before is on this task." onPick={vi.fn()} />)
    expect(screen.getByRole('group', { name: 'Used before:' })).toHaveTextContent('Every label used before is on this task.')
  })

  it('disables the tags while disabled, but not the fold button', () => {
    render(<LabelSuggestions labels={many} disabled onPick={vi.fn()} />)
    expect(screen.getByRole('button', { name: `Add label ${many[0]}` })).toBeDisabled()
    expect(screen.getByRole('button', { name: '+3 more' })).toBeEnabled()
  })
})
