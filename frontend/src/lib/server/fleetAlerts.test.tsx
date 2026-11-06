import {act, render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {evaluate, fleetAlertConfig, keepFleetHosts, reportFleet, resetFleetAlerts, setFleetAlertConfig, setFleetNotifier, DEFAULT_ALERT_CONFIG, type FleetAlert} from './fleetAlerts'
import {FleetMonitor} from './FleetMonitor'
import {FleetPage} from './FleetPage'
import {host, makeFake, Providers} from '../../test/fakeBackend'
import type {ServerFleetHost} from '../types'

const H = {id: 'a', label: 'alpha'}
const ok = (over: Partial<ServerFleetHost['overview']> = {}, rest: Partial<ServerFleetHost> = {}): ServerFleetHost => ({
  host_id: 'a', error: '', failed_units: 0, latency_ms: 5,
  overview: {disk_used: 50, disk_total: 100, mem_used: 50, mem_total: 100, cpu_pct: 5, load: [0, 0, 0], uptime_sec: 1, ...over} as ServerFleetHost['overview'], ...rest,
})
const down: ServerFleetHost = {host_id: 'a', error: 'timeout', overview: {} as ServerFleetHost['overview'], failed_units: 0, latency_ms: 0}

beforeEach(() => { localStorage.clear(); resetFleetAlerts() })

describe('fleet alert rules', () => {
  it('raises disk, memory, service and certificate alerts over their limits only', () => {
    const cfg = {...DEFAULT_ALERT_CONFIG}
    expect(evaluate(cfg, H, ok())).toEqual([])
    const kinds = evaluate(cfg, H, ok({disk_used: 95, mem_used: 96}, {failed_units: 2, cert_days: 3})).map(a => [a.kind, a.value])
    expect(kinds).toEqual([['disk', 95], ['mem', 96], ['service', 2], ['cert', 3]])
    expect(evaluate(cfg, H, ok({}, {cert_days: 30}))).toEqual([])
    expect(evaluate(cfg, H, ok({}, {cert_days: null}))).toEqual([])
    expect(evaluate(cfg, H, ok({}, {cert_days: -2}))[0].value).toBe(-2)
  })
})

describe('fleet alert store', () => {
  it('counts a host as down after two failed checks in a row, and clears it when it answers', () => {
    const seen: FleetAlert[][] = []
    setFleetAlertConfig({...fleetAlertConfig(), notify: true})
    setFleetNotifier((_l, a) => seen.push(a))
    reportFleet(H, down)
    expect(seen).toHaveLength(0)
    reportFleet(H, down)
    expect(seen.map(a => a.map(x => x.kind))).toEqual([['down']])
    reportFleet(H, down) // still down: not told again
    expect(seen).toHaveLength(1)
    reportFleet(H, ok())
    reportFleet(H, down)
    reportFleet(H, down)
    expect(seen).toHaveLength(2) // down again after it was well: told again
  })

  it('tells about a new alert once, and about one that came back', () => {
    const seen: string[] = []
    setFleetAlertConfig({...fleetAlertConfig(), notify: true})
    setFleetNotifier((_l, a) => seen.push(a.map(x => x.kind).join()))
    reportFleet(H, ok({disk_used: 95}))
    reportFleet(H, ok({disk_used: 96}))
    reportFleet(H, ok({disk_used: 96}, {failed_units: 1}))
    reportFleet(H, ok())
    reportFleet(H, ok({disk_used: 95}))
    expect(seen).toEqual(['disk', 'service', 'disk'])
  })

  it('says nothing when notifications are off, and forgets hosts that are gone', () => {
    const seen: string[] = []
    setFleetNotifier(() => seen.push('x'))
    reportFleet(H, ok({disk_used: 99}))
    expect(seen).toEqual([])
    keepFleetHosts([])
    setFleetAlertConfig({...fleetAlertConfig(), notify: true})
    reportFleet(H, ok({disk_used: 99}))
    expect(seen).toEqual(['x']) // the alert was forgotten, so it is new again
  })
})

describe('Fleet page alerts', () => {
  const a = host({id: 'a', label: 'alpha', hostname: 'a.example.com'})
  const b = host({id: 'b', label: 'bravo', hostname: 'b.example.com'})

  it('lists the alerts with the words of each and opens the panel', async () => {
    const be = makeFake({hosts: [a, b]})
    be.state.server.overview.disk_used = be.state.server.overview.disk_total * 0.95
    const onPanel = vi.fn()
    render(<Providers be={be}><FleetPage hosts={[a, b]} onPanel={onPanel} /></Providers>)
    expect(await screen.findByText('alpha · disk 95% full')).toBeInTheDocument()
    expect(screen.getByText('bravo · disk 95% full')).toBeInTheDocument()
    expect(screen.getByText('alpha · failed services: 1')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Open the panel of alpha'}))
    expect(onPanel).toHaveBeenCalledWith(a)
  })

  it('turning notifications on sends a test one, and the limits are remembered', async () => {
    const be = makeFake({hosts: [a]})
    render(<Providers be={be}><FleetPage hosts={[a]} onPanel={vi.fn()} /></Providers>)
    await userEvent.click(await screen.findByRole('checkbox', {name: 'Desktop notifications'}))
    await waitFor(() => expect(be.state.calls.filter(c => c[0] === 'server.notify')).toHaveLength(1))
    expect(fleetAlertConfig().notify).toBe(true)
    const disk = screen.getByLabelText('Disk alert limit in percent')
    await userEvent.clear(disk)
    await userEvent.type(disk, '80')
    await userEvent.tab()
    expect(fleetAlertConfig().disk).toBe(80)
    expect(JSON.parse(localStorage.getItem('vutty.fleet.alerts')!).disk).toBe(80)
  })
})

describe('Fleet: one action on several servers', () => {
  const a = host({id: 'a', label: 'alpha', hostname: 'a.example.com'})
  const b = host({id: 'b', label: 'bravo', hostname: 'b.example.com'})
  const calls = (be: ReturnType<typeof makeFake>, n: string) => be.state.calls.filter(c => c[0] === n)

  it('looks at the updates of the ticked servers only, without asking', async () => {
    const be = makeFake({hosts: [a, b]})
    render(<Providers be={be}><FleetPage hosts={[a, b]} onPanel={vi.fn()} /></Providers>)
    const look = await screen.findByRole('button', {name: 'Look at the updates'})
    expect(look).toBeDisabled()
    await userEvent.click(screen.getByRole('checkbox', {name: 'Select alpha'}))
    await userEvent.click(look)
    await waitFor(() => expect(calls(be, 'server.fleetAction')).toEqual([['server.fleetAction', 'a', 'updates']]))
    expect(await screen.findByText('12 updates, 3 security')).toBeInTheDocument()
    expect(screen.getByText('Needs a reboot')).toBeInTheDocument()
  })

  it('renews certificates on all servers after asking, and shows the one that failed', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const be = makeFake({hosts: [a, b]})
    be.state.server.fleetErrors = {b: 'certbot is not installed on this server'}
    render(<Providers be={be}><FleetPage hosts={[a, b]} onPanel={vi.fn()} /></Providers>)
    await userEvent.click(await screen.findByRole('checkbox', {name: 'Select all'}))
    expect(screen.getByText('2 selected')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Renew certificates'}))
    expect(ask).toHaveBeenLastCalledWith(expect.stringContaining('Renew the certificates on 2 servers?'))
    expect(calls(be, 'server.fleetAction')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Renew certificates'}))
    await waitFor(() => expect(calls(be, 'server.fleetAction').map(c => c.slice(1).join(':')).sort()).toEqual(['a:renew', 'b:renew']))
    expect(await screen.findByText('Done')).toBeInTheDocument()
    const box = screen.getByRole('button', {name: 'Do on several servers'}).closest('section') as HTMLElement
    expect(await within(box).findByText('certbot is not installed on this server')).toBeInTheDocument()
    ask.mockRestore()
  })
})

describe('Fleet monitor', () => {
  const a = host({id: 'a', label: 'alpha', hostname: 'a.example.com'})

  it('checks the hosts while the Fleet page is not shown and hands new alerts to the system', async () => {
    setFleetAlertConfig({...fleetAlertConfig(), notify: true})
    const be = makeFake({hosts: [a]})
    render(<Providers be={be}><FleetMonitor hosts={[a]} fleetShown={false} /></Providers>)
    await waitFor(() => expect(be.state.calls.filter(c => c[0] === 'server.fleetHost')).toHaveLength(1))
    await waitFor(() => expect(be.state.calls.filter(c => c[0] === 'server.notify')).toEqual([['server.notify', 'alpha', 'failed services: 1']]))
  })

  it('does not check on its own while the page is shown or notifications are off', async () => {
    const be = makeFake({hosts: [a]})
    const {rerender} = render(<Providers be={be}><FleetMonitor hosts={[a]} fleetShown={false} /></Providers>)
    await act(async () => { await new Promise(r => setTimeout(r, 30)) })
    setFleetAlertConfig({...fleetAlertConfig(), notify: true})
    rerender(<Providers be={be}><FleetMonitor hosts={[a]} fleetShown /></Providers>)
    await act(async () => { await new Promise(r => setTimeout(r, 30)) })
    expect(be.state.calls.filter(c => c[0] === 'server.fleetHost')).toHaveLength(0)
  })
})
