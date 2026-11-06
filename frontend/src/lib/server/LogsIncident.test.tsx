import {act, cleanup, render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {ServerPanel} from './ServerPanel'
import {barsOf} from './Incident'
import {host, makeFake, Providers} from '../../test/fakeBackend'

type Fake = ReturnType<typeof makeFake>
const web = host({id: 'h1', label: 'web-1', hostname: 'w1.example.com', username: 'ubuntu', port: 22})
beforeEach(() => localStorage.clear())
afterEach(() => { cleanup(); vi.restoreAllMocks() })
const calls = (be: Fake, n: string) => be.state.calls.filter(c => c[0] === n)

function setup(init: Parameters<typeof makeFake>[0] = {}) {
  const be = makeFake({hosts: [web], ...init})
  render(<Providers be={be}><ServerPanel host={web} visible /></Providers>)
  return be
}
const open = async (name: string) => { await screen.findByText(/up 41d 0h/); await userEvent.click(screen.getByRole('tab', {name})) }
const keyOf = (be: Fake) => calls(be, 'server.logsFollowStart').at(-1)!.at(-1) as string

describe('Logs: Follow streams', () => {
  it('starts a stream on the host, shows the lines that come, reads nothing again, and stops it', async () => {
    const be = setup()
    await open('Logs')
    await screen.findByText(/ECONNREFUSED/)
    const reads = calls(be, 'server.logs').length
    await userEvent.click(screen.getByRole('button', {name: 'Follow'}))
    await waitFor(() => expect(calls(be, 'server.logsFollowStart')).toHaveLength(1))
    expect(calls(be, 'server.logsFollowStart')[0].slice(1, 5)).toEqual(['', '', 300, ''])
    const key = keyOf(be)
    expect(key).toMatch(/^[A-Za-z0-9-]{8,64}$/)
    act(() => be.emit('server:log:' + key, {lines: ['2026-11-04T10:00:00+0700 host app: streamed one', '2026-11-04T10:00:01+0700 host app: streamed two'], dropped: 0}))
    expect(await screen.findByText(/streamed two/)).toBeInTheDocument()
    expect(screen.queryByText(/ECONNREFUSED/)).toBeNull() // the stream starts with its own last lines
    expect(calls(be, 'server.logs')).toHaveLength(reads) // and nothing is read again
    await userEvent.click(screen.getByRole('button', {name: 'Follow'}))
    await waitFor(() => expect(calls(be, 'server.logsFollowStop').at(-1)).toEqual(['server.logsFollowStop', key]))
    await waitFor(() => expect(calls(be, 'server.logs').length).toBeGreaterThan(reads)) // read again: what was streamed is newer than the last reading
  })
  it('says so and reads again every two seconds when the stream cannot start', async () => {
    const be = setup()
    be.state.server.followFails = 'journalctl: not found'
    await open('Logs')
    await screen.findByText(/ECONNREFUSED/)
    vi.useFakeTimers({shouldAdvanceTime: true})
    try {
      await userEvent.click(screen.getByRole('button', {name: 'Follow'}))
      expect(await screen.findByText(/Could not follow the log live \(journalctl: not found\)/)).toBeInTheDocument()
      const n = calls(be, 'server.logs').length
      await act(async () => { await vi.advanceTimersByTimeAsync(4100) })
      expect(calls(be, 'server.logs').length).toBeGreaterThanOrEqual(n + 2)
    } finally { vi.useRealTimers() }
  })
  it('stops the stream when another source is chosen, and starts the next with its own key', async () => {
    const be = setup()
    await open('Logs')
    await screen.findByText(/ECONNREFUSED/)
    await userEvent.click(screen.getByRole('button', {name: 'Follow'}))
    await waitFor(() => expect(calls(be, 'server.logsFollowStart')).toHaveLength(1))
    const first = keyOf(be)
    await userEvent.click(screen.getByRole('combobox', {name: /^Log source/}))
    await userEvent.click(await screen.findByRole('option', {name: /api/}))
    await waitFor(() => expect(calls(be, 'server.logsFollowStart')).toHaveLength(2))
    expect(calls(be, 'server.logsFollowStop').map(c => c[1])).toContain(first)
    expect(calls(be, 'server.logsFollowStart')[1].slice(1, 3)).toEqual(['container', 'api'])
    expect(keyOf(be)).not.toBe(first)
  })
})

describe('Logs: a time range, the boot before, and the lines around one', () => {
  it('reads a range of its own in the time of the server, and cannot be followed', async () => {
    const be = setup()
    await open('Logs')
    await screen.findByText(/ECONNREFUSED/)
    await userEvent.click(screen.getByRole('combobox', {name: /^Time range/}))
    await userEvent.click(await screen.findByRole('option', {name: /Choose a range/}))
    // the clock of the fake server is 2026-11-04 10:00 at +07:00: the first range is the hour before
    await waitFor(() => expect(calls(be, 'server.logsAt').at(-1)).toEqual(['server.logsAt', '', '', 300, '2026-11-04T02:00:00Z', '2026-11-04T03:00:00Z', false]))
    expect(await screen.findByText(/Server time \(\+07, UTC\+07:00\)/)).toBeInTheDocument()
    expect(screen.getByRole('button', {name: 'Follow'})).toBeDisabled()
    const from = screen.getByLabelText('From') as HTMLInputElement
    expect(from.value).toBe('2026-11-04T09:00')
    await userEvent.clear(from)
    await userEvent.type(from, '2026-11-03T02:40')
    await userEvent.click(screen.getByRole('button', {name: 'Show'}))
    await waitFor(() => expect(calls(be, 'server.logsAt').at(-1)!.slice(4, 6)).toEqual(['2026-11-03T02:40:00+07:00', '2026-11-04T10:00:00+07:00']))
  })
  it('keeps the range when another source is chosen, and goes back to the latest lines with Any time', async () => {
    const be = setup()
    await open('Logs')
    await screen.findByText(/ECONNREFUSED/)
    await userEvent.click(screen.getByRole('combobox', {name: /^Time range/}))
    await userEvent.click(await screen.findByRole('option', {name: /Choose a range/}))
    await waitFor(() => expect(calls(be, 'server.logsAt')).toHaveLength(1))
    await userEvent.click(screen.getByRole('combobox', {name: /^Log source/}))
    await userEvent.click(await screen.findByRole('option', {name: /api/}))
    await waitFor(() => expect(calls(be, 'server.logsAt').at(-1)!.slice(1, 3)).toEqual(['container', 'api']))
    await userEvent.click(screen.getByRole('combobox', {name: /^Time range/}))
    await userEvent.click(await screen.findByRole('option', {name: 'Any time'}))
    await waitFor(() => expect(screen.queryByLabelText('From')).toBeNull())
    await waitFor(() => expect(calls(be, 'server.logs').at(-1)!.slice(1, 3)).toEqual(['container', 'api']))
  })
  it('reads the boot before this one for the journal only', async () => {
    const be = setup()
    await open('Logs')
    await screen.findByText(/ECONNREFUSED/)
    await userEvent.click(screen.getByRole('button', {name: 'Previous boot'}))
    await waitFor(() => expect(calls(be, 'server.logsAt').at(-1)).toEqual(['server.logsAt', '', '', 300, '', '', true]))
    expect(await screen.findByText(/Showing the boot before this one/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('combobox', {name: /^Log source/}))
    await userEvent.click(await screen.findByRole('option', {name: /api/}))
    await waitFor(() => expect(screen.queryByRole('button', {name: 'Previous boot'})).toBeNull()) // Docker keeps no log by boot
  })
  it('asks for the minutes around a line that was clicked', async () => {
    const be = setup()
    await open('Logs')
    await userEvent.click(await screen.findByText(/ECONNREFUSED/))
    const bar = await screen.findByRole('group', {name: 'Around the picked line'})
    await userEvent.click(within(bar).getByRole('button', {name: 'Show ±5 min around it'}))
    // 09:41:07+0700 is 02:41:07 UTC
    await waitFor(() => expect(calls(be, 'server.logsAt').at(-1)).toEqual(['server.logsAt', '', '', 300, '2026-11-04T02:36:07Z', '2026-11-04T02:46:07Z', false]))
    expect(screen.queryByRole('group', {name: 'Around the picked line'})).toBeNull()
  })
  it('keeps three lines around each match with Context, and marks the holes', async () => {
    const lines = Array.from({length: 20}, (_, i) => `2026-11-04T09:00:${String(i).padStart(2, '0')}+0700 host app: line ${i}${i === 3 || i === 15 ? ' BOOM' : ''}`)
    const be = makeFake({hosts: [web]})
    be.state.server.logs = lines.join('\n')
    render(<Providers be={be}><ServerPanel host={web} visible /></Providers>)
    await open('Logs')
    await screen.findByText(/line 0/)
    expect(screen.getByRole('button', {name: 'Context'})).toBeDisabled()
    await userEvent.type(screen.getByRole('textbox', {name: 'Filter log lines'}), 'BOOM')
    await userEvent.click(screen.getByRole('button', {name: 'Context'}))
    expect(screen.getByText(/line 0$/)).toBeInTheDocument() // 3 before line 3
    expect(screen.queryByText(/line 8$/)).toBeNull()
    expect(screen.getByText(/line 18/)).toBeInTheDocument()
    expect(screen.getAllByText('···')).toHaveLength(1)
  })
})

describe('Incident tab', () => {
  const incident = {
    events: [
      {time: '2026-11-04T02:38:00Z', kind: 'oom' as const, text: 'host kernel: Out of memory: Killed process 1234 (node)'},
      {time: '2026-11-04T02:39:00Z', kind: 'restart' as const, text: 'host systemd[1]: app.service: Scheduled restart job, restart counter is at 3.'},
    ],
    errors: [{minute: '2026-11-04T02:38:00Z', count: 14}, {minute: '2026-11-04T02:39:00Z', count: 3}],
    containers: [{name: 'api', restarts: 2, oom: true, exit_code: 137, running: true, started: '2026-11-04T02:40:00Z', finished: ''}],
    persistent: false, oldest: '2026-11-04T02:30:00Z', limited: false, has_docker: true, truncated: false,
  }
  it('lists what the server wrote down, in the time of the server, and opens the log at that moment', async () => {
    const be = setup()
    be.state.server.incident = incident
    await open('Incident')
    expect(await screen.findByText('2026-11-04 09:38:00')).toBeInTheDocument() // 02:38 UTC at +07:00
    expect(calls(be, 'server.incident').at(-1)).toEqual(['server.incident', '2026-11-03T03:00:00Z', '2026-11-04T03:00:00Z'])
    expect(screen.getAllByText('Out of memory').length).toBeGreaterThan(0)
    expect(screen.getByText(/The journal starts at 2026-11-04 09:30:00/)).toBeInTheDocument()
    expect(screen.getByText(/kept in memory only/)).toBeInTheDocument()
    const row = screen.getByText(/Killed process 1234/).closest('tr') as HTMLElement
    await userEvent.click(within(row).getByRole('button', {name: 'Logs'}))
    // the Logs tab opens on the 5 minutes around it
    await waitFor(() => expect(calls(be, 'server.logsAt').at(-1)).toEqual(['server.logsAt', '', '', 300, '2026-11-04T02:33:00Z', '2026-11-04T02:43:00Z', false]))
  })
  it('opens the log of the 15 minutes of a bar of errors (a day is drawn in at most 96 bars)', async () => {
    const be = setup()
    be.state.server.incident = incident
    await open('Incident')
    await userEvent.click(await screen.findByRole('button', {name: /09:30:00: 17 errors/}))
    await waitFor(() => expect(calls(be, 'server.logsAt').at(-1)!.slice(4)).toEqual(['2026-11-04T02:30:00Z', '2026-11-04T02:45:00Z', false]))
  })
  it('keeps the journal on disk only after asking', async () => {
    const be = setup()
    be.state.server.incident = incident
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    await open('Incident')
    await userEvent.click(await screen.findByRole('button', {name: 'Keep it on disk'}))
    expect(ask).toHaveBeenCalled()
    expect(calls(be, 'server.journalKeep')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Keep it on disk'}))
    await waitFor(() => expect(calls(be, 'server.journalKeep')).toHaveLength(1))
    await waitFor(() => expect(screen.queryByRole('button', {name: 'Keep it on disk'})).toBeNull())
  })
  it('says when nothing happened', async () => {
    setup()
    await open('Incident')
    expect(await screen.findByText(/No failed service, restart, memory kill or reboot/)).toBeInTheDocument()
    expect(screen.getByText(/No error lines in this time range/)).toBeInTheDocument()
  })
})

describe('the strip of errors', () => {
  it('puts the minutes into at most 96 bars across the range', () => {
    const from = Date.parse('2026-11-04T00:00:00Z'), day = 86400000
    const bars = barsOf([{minute: '2026-11-04T00:00:00Z', count: 2}, {minute: '2026-11-04T23:59:00Z', count: 5}, {minute: '2026-11-05T01:00:00Z', count: 9}], from, from + day)
    expect(bars.length).toBeLessThanOrEqual(96)
    expect(bars.reduce((s, b) => s + b.count, 0)).toBe(7) // the one outside the range is not counted
    expect(bars[0].count).toBe(2)
    expect(bars.at(-1)!.count).toBe(5)
    expect(barsOf([], from, from + 60000)).toHaveLength(1)
  })
})
