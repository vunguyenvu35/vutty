import {useState} from 'react'
import {render, screen} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {describe, expect, it} from 'vitest'
import {MultiSelect} from './MultiSelect'

const OPTIONS = [{value: 'a', label: 'Alpha', hint: '3'}, {value: 'b', label: 'Beta'}, {value: 'c', label: 'Gamma'}]

function Host({start = []}: {start?: string[]}) {
  const [values, setValues] = useState(start)
  return <MultiSelect label="Letters" values={values} onChange={setValues} options={OPTIONS} allLabel="Every letter" />
}
const button = () => screen.getByRole('combobox', {name: /^Letters/})

describe('MultiSelect', () => {
  it('says what is ticked: all, the names of one or two, then a count', async () => {
    render(<Host />)
    expect(button()).toHaveAccessibleName('Letters, Every letter')
    await userEvent.click(button())
    await userEvent.click(screen.getByRole('option', {name: /Alpha/}))
    expect(button()).toHaveAccessibleName('Letters, Alpha')
    await userEvent.click(screen.getByRole('option', {name: /Beta/}))
    expect(button()).toHaveAccessibleName('Letters, Alpha, Beta')
    await userEvent.click(screen.getByRole('option', {name: /Gamma/}))
    expect(button()).toHaveAccessibleName('Letters, 3 selected')
  })

  it('stays open while options are ticked and unticked, and the first row clears every tick', async () => {
    render(<Host start={['a']} />)
    await userEvent.click(button())
    expect(screen.getByRole('listbox')).toHaveAttribute('aria-multiselectable', 'true')
    expect(screen.getByRole('option', {name: /Alpha/})).toHaveAttribute('aria-selected', 'true')
    await userEvent.click(screen.getByRole('option', {name: /Beta/}))
    expect(screen.getByRole('listbox')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('option', {name: /Alpha/}))
    expect(screen.getByRole('option', {name: /Alpha/})).toHaveAttribute('aria-selected', 'false')
    await userEvent.click(screen.getByRole('option', {name: 'Every letter'}))
    expect(screen.getByRole('option', {name: 'Every letter'})).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('option', {name: /Beta/})).toHaveAttribute('aria-selected', 'false')
  })

  it('works from the keyboard: arrows move, Space ticks, Escape closes', async () => {
    render(<Host />)
    button().focus()
    await userEvent.keyboard('{Enter}{ArrowDown}{ }{ArrowDown}{ }')
    expect(screen.getByRole('listbox')).toBeInTheDocument()
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('listbox')).toBeNull()
    expect(button()).toHaveAccessibleName('Letters, Alpha, Beta')
  })
})
