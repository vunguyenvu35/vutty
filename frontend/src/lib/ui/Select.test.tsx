import {render, screen, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {useState} from 'react'
import {Select, SelectOption} from './Select'

const OPTS: SelectOption[] = [
  {value: '', label: 'None'},
  {value: 'dev', label: 'dev', hint: 'development'},
  {value: 'stg', label: 'staging'},
  {value: 'off', label: 'disabled one', disabled: true},
  {value: 'prod', label: 'prod'},
]

function Harness({initial = '', onChange = vi.fn(), options = OPTS, ...rest}: {initial?: string; onChange?: (v: string) => void; options?: SelectOption[]; placeholder?: string; disabled?: boolean; searchable?: boolean}) {
  const [v, setV] = useState(initial)
  return <Select label="Env" value={v} options={options} onChange={x => { setV(x); onChange(x) }} {...rest} />
}
const trigger = () => screen.getByRole('combobox')

describe('Select', () => {
  it('shows the current label and exposes combobox semantics', () => {
    render(<Harness initial="dev" />)
    expect(trigger()).toHaveTextContent('dev')
    expect(trigger()).toHaveAttribute('aria-haspopup', 'listbox')
    expect(trigger()).toHaveAttribute('aria-expanded', 'false')
    expect(trigger()).toHaveAccessibleName('Env, dev')
  })

  it('opens a custom listbox (not the native popup) with hints and a check on the selected option', async () => {
    render(<Harness initial="dev" />)
    await userEvent.click(trigger())
    const list = screen.getByRole('listbox', {name: 'Env'})
    expect(document.body.contains(list)).toBe(true)
    expect(within(list).getAllByRole('option')).toHaveLength(5)
    expect(within(list).getByRole('option', {name: /dev/})).toHaveAttribute('aria-selected', 'true')
    expect(within(list).getByText('development')).toBeInTheDocument()
    expect(trigger()).toHaveAttribute('aria-expanded', 'true')
  })

  it('click selects, closes and calls onChange once', async () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    await userEvent.click(trigger())
    await userEvent.click(screen.getByRole('option', {name: 'prod'}))
    expect(onChange).toHaveBeenCalledTimes(1)
    expect(onChange).toHaveBeenCalledWith('prod')
    expect(screen.queryByRole('listbox')).toBeNull()
    expect(trigger()).toHaveTextContent('prod')
    expect(trigger()).toHaveFocus()
  })

  it('choosing the current value does not fire onChange', async () => {
    const onChange = vi.fn()
    render(<Harness initial="dev" onChange={onChange} />)
    await userEvent.click(trigger())
    await userEvent.click(screen.getByRole('option', {name: /dev/}))
    expect(onChange).not.toHaveBeenCalled()
  })

  it('disabled options cannot be chosen', async () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    await userEvent.click(trigger())
    await userEvent.click(screen.getByRole('option', {name: 'disabled one'}))
    expect(onChange).not.toHaveBeenCalled()
    expect(screen.getByRole('listbox')).toBeInTheDocument()
  })

  it('keyboard: arrows skip disabled options, Enter selects, Esc closes and keeps the value', async () => {
    const onChange = vi.fn()
    render(<Harness initial="stg" onChange={onChange} />)
    trigger().focus()
    await userEvent.keyboard('{ArrowDown}')
    expect(screen.getByRole('listbox')).toHaveFocus()
    await userEvent.keyboard('{ArrowDown}') // staging -> (skips "disabled one") -> prod
    await userEvent.keyboard('{Enter}')
    expect(onChange).toHaveBeenCalledWith('prod')

    await userEvent.keyboard('{ArrowUp}')
    await userEvent.keyboard('{Home}{Escape}')
    expect(screen.queryByRole('listbox')).toBeNull()
    expect(trigger()).toHaveTextContent('prod')
  })

  it('End and Home jump to the ends; type-ahead finds an option', async () => {
    const onChange = vi.fn()
    render(<Harness initial="dev" onChange={onChange} />)
    await userEvent.click(trigger())
    await userEvent.keyboard('{End}{Enter}')
    expect(onChange).toHaveBeenLastCalledWith('prod')
    await userEvent.click(trigger())
    await userEvent.keyboard('st{Enter}')
    expect(onChange).toHaveBeenLastCalledWith('stg')
  })

  it('Space and Enter on the trigger open it; Tab closes it', async () => {
    render(<Harness />)
    trigger().focus()
    await userEvent.keyboard(' ')
    expect(screen.getByRole('listbox')).toBeInTheDocument()
    await userEvent.keyboard('{Tab}')
    expect(screen.queryByRole('listbox')).toBeNull()
    trigger().focus()
    await userEvent.keyboard('{Enter}')
    expect(screen.getByRole('listbox')).toBeInTheDocument()
  })

  it('closes on an outside click and toggles on a second trigger click', async () => {
    render(<div><Harness /><button>elsewhere</button></div>)
    await userEvent.click(trigger())
    await userEvent.click(screen.getByText('elsewhere'))
    expect(screen.queryByRole('listbox')).toBeNull()
    await userEvent.click(trigger())
    await userEvent.click(trigger())
    expect(screen.queryByRole('listbox')).toBeNull()
  })

  it('Escape closes the menu without letting the event reach a parent that would close a panel', async () => {
    const parent = vi.fn()
    render(<div onKeyDown={parent}><Harness /></div>)
    await userEvent.click(trigger())
    await userEvent.keyboard('{Escape}')
    expect(parent).not.toHaveBeenCalled()
  })

  it('keeps a hidden native <select> in sync, so label lookups and form tooling still work', async () => {
    const onChange = vi.fn()
    render(<Harness onChange={onChange} />)
    const native = screen.getByLabelText('Env') as HTMLSelectElement
    expect(native.tagName).toBe('SELECT')
    expect(native).toHaveAttribute('aria-hidden', 'true')
    await userEvent.selectOptions(native, 'staging')
    expect(onChange).toHaveBeenCalledWith('stg')
    expect(trigger()).toHaveTextContent('staging')
  })

  it('shows a placeholder when the value matches no option', () => {
    render(<Harness initial="" options={OPTS.slice(1)} placeholder="Pick one" />)
    expect(trigger()).toHaveTextContent('Pick one')
    expect(trigger()).toHaveAccessibleName('Env, Pick one')
  })

  it('a disabled select cannot be opened', async () => {
    render(<Harness disabled />)
    await userEvent.click(trigger())
    expect(screen.queryByRole('listbox')).toBeNull()
    expect(trigger()).toBeDisabled()
  })
})

describe('Select that can be searched', () => {
  const PODS: SelectOption[] = [
    {value: 'a', label: 'binlog-reader-cron-main-0', hint: 'Pods · vl24hv2-qc'},
    {value: 'b', label: 'service-employer-6bf58f98dd-q8lfn', hint: 'Pods · vl24hv2-qc'},
    {value: 'c', label: 'service-employer-queue-58bfcd897f-hzmb6', hint: 'Pods · vl24hv2-qc'},
    {value: 'd', label: 'nginx-ingress', hint: 'Pods · edge'},
  ]
  const box = () => screen.getByRole('searchbox')
  const shown = () => screen.queryAllByRole('option').map(o => o.querySelector('.opt-text')!.firstChild!.textContent)

  it('has no box unless it is asked for', async () => {
    render(<Harness initial="dev" />)
    await userEvent.click(trigger())
    expect(screen.queryByRole('searchbox')).toBeNull()
  })

  it('puts the cursor in the box and keeps only the options that contain what is typed, in the name or in the small print', async () => {
    render(<Harness options={PODS} initial="a" searchable />)
    await userEvent.click(trigger())
    expect(box()).toHaveFocus()
    expect(shown()).toHaveLength(4)
    await userEvent.type(box(), 'employer')
    expect(shown()).toEqual(['service-employer-6bf58f98dd-q8lfn', 'service-employer-queue-58bfcd897f-hzmb6'])
    await userEvent.clear(box())
    await userEvent.type(box(), 'edge') // the hint
    expect(shown()).toEqual(['nginx-ingress'])
    await userEvent.clear(box())
    await userEvent.type(box(), 'service queue') // every word, in any order
    expect(shown()).toEqual(['service-employer-queue-58bfcd897f-hzmb6'])
  })

  it('chooses the first match with Enter, or the one the arrows moved to, and says so when nothing matches', async () => {
    const onChange = vi.fn()
    render(<Harness options={PODS} initial="a" searchable onChange={onChange} />)
    await userEvent.click(trigger())
    await userEvent.type(box(), 'zzz')
    expect(screen.getByText('No match')).toBeInTheDocument()
    await userEvent.keyboard('{Enter}')
    expect(onChange).not.toHaveBeenCalled() // nothing to choose
    await userEvent.clear(box())
    await userEvent.type(box(), 'employer{ArrowDown}{Enter}')
    expect(onChange).toHaveBeenCalledWith('c')
    expect(trigger()).toHaveTextContent('service-employer-queue')
  })

  it('takes a space as a letter of the search, starts empty each time, and closes with Escape', async () => {
    render(<Harness options={PODS} initial="a" searchable />)
    await userEvent.click(trigger())
    await userEvent.type(box(), 'in x')
    expect(box()).toHaveValue('in x')
    await userEvent.keyboard('{Escape}')
    expect(screen.queryByRole('searchbox')).toBeNull()
    await userEvent.click(trigger())
    expect(box()).toHaveValue('')
    expect(shown()).toHaveLength(4)
  })
})

