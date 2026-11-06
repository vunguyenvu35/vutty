import {render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {ServerPanel} from './ServerPanel'
import {host, makeFake, Providers} from '../../test/fakeBackend'

const web = host({id: 'h1', label: 'web-1', hostname: 'w1.example.com', username: 'ubuntu', port: 22})
beforeEach(() => localStorage.clear())
afterEach(() => vi.restoreAllMocks())

function setup() {
  const be = makeFake({hosts: [web]})
  render(<Providers be={be}><ServerPanel host={web} visible /></Providers>)
  return be
}
const rail = () => screen.getByRole('navigation', {name: 'Server sections'})
const names = (el: HTMLElement) => within(el).getAllByRole('tab').map(t => t.getAttribute('aria-label'))

describe('the section list', () => {
  it('has every section once, in three groups of what to look at, what runs and what to look after', async () => {
    setup()
    await screen.findByText(/up 41d 0h/)
    expect(names(within(rail()).getByRole('tablist', {name: 'Monitor'}))).toEqual(['Overview', 'Processes', 'Logs', 'Incident'])
    expect(names(within(rail()).getByRole('tablist', {name: 'Run'}))).toEqual(['Services', 'Docker', 'Kubernetes', 'Web', 'Schedule'])
    expect(names(within(rail()).getByRole('tablist', {name: 'Manage'}))).toEqual(['Ports & firewall', 'Security', 'Users', 'Maintenance'])
    expect(screen.getByRole('tab', {name: 'Overview'})).toHaveAttribute('aria-selected', 'true')
    await userEvent.click(screen.getByRole('tab', {name: 'Users'}))
    expect(screen.getByRole('tab', {name: 'Users'})).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('tab', {name: 'Overview'})).toHaveAttribute('aria-selected', 'false')
  })
  it('marks the section of each thing that needs attention, and the group they are in', async () => {
    setup() // the fake server has a failed service
    await screen.findByText(/up 41d 0h/)
    const services = await waitFor(() => { const b = screen.getByRole('tab', {name: 'Services'}); expect(within(b).getByText('1')).toBeInTheDocument(); return b })
    expect(services).toHaveAccessibleDescription('1 item needs attention')
    const run = within(rail()).getByRole('tablist', {name: 'Run'})
    expect(within(run).getAllByText('1').length).toBeGreaterThanOrEqual(2) // the section and its group
    // the name of a section stays its own: a mark does not become part of it
    expect(screen.getByRole('tab', {name: 'Services'})).toBeInTheDocument()
  })
  it('shows icons only when asked, and remembers it', async () => {
    setup()
    await screen.findByText(/up 41d 0h/)
    await userEvent.click(screen.getByRole('button', {name: 'Show icons only'}))
    expect(rail().parentElement).toHaveClass('slim')
    expect(screen.getByRole('tab', {name: 'Logs'})).toHaveAttribute('title', expect.stringContaining('Logs'))
    expect(localStorage.getItem('vutty.panel.rail')).toBe('1')
    await userEvent.click(screen.getByRole('button', {name: 'Show section names'}))
    expect(rail().parentElement).not.toHaveClass('slim')
    expect(localStorage.getItem('vutty.panel.rail')).toBe('0')
  })
  it('reads what needs attention again when the Overview is opened again', async () => {
    const be = setup()
    await screen.findByText(/up 41d 0h/)
    await waitFor(() => expect(be.state.calls.filter(c => c[0] === 'server.services').length).toBeGreaterThan(0))
    const before = be.state.calls.filter(c => c[0] === 'server.services').length
    await userEvent.click(screen.getByRole('tab', {name: 'Logs'}))
    expect(be.state.calls.filter(c => c[0] === 'server.services').length).toBeGreaterThanOrEqual(before)
    const mid = be.state.calls.filter(c => c[0] === 'server.services').length
    await userEvent.click(screen.getByRole('tab', {name: 'Overview'}))
    await waitFor(() => expect(be.state.calls.filter(c => c[0] === 'server.services').length).toBeGreaterThan(mid))
  })
})
