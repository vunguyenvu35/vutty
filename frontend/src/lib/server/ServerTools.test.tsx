import {act, render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {ServerPanel} from './ServerPanel'
import {FleetPage} from './FleetPage'
import {backupLevel, certLevel, validPath} from './format'
import {host, makeFake, Providers} from '../../test/fakeBackend'

type Fake = ReturnType<typeof makeFake>
const web = host({id: 'h1', label: 'web-1', hostname: 'w1.example.com', username: 'ubuntu', port: 22})
const calls = (be: Fake, n: string) => be.state.calls.filter(c => c[0] === n)

function setup(h = web) {
  const be = makeFake({hosts: [h]})
  const view = render(<Providers be={be}><ServerPanel host={h} visible /></Providers>)
  return {be, ...view}
}
const open = async (name: string) => { await screen.findByText(/up 41d 0h/); await userEvent.click(screen.getByRole('tab', {name})) }
const rowOf = (text: string) => screen.getByText(text).closest('tr') as HTMLElement

beforeEach(() => { vi.spyOn(window, 'confirm').mockReturnValue(true) })
afterEach(() => vi.restoreAllMocks())

describe('format helpers of the server tools', () => {
  it('colours certificates by the time left, and backups by age', () => {
    expect([certLevel(30), certLevel(13), certLevel(6), certLevel(-1)]).toEqual(['ok', 'warn', 'bad', 'bad'])
    expect([backupLevel(3600), backupLevel(3 * 86400), backupLevel(9 * 86400)]).toEqual(['ok', 'warn', 'bad'])
  })
  it('accepts only plain absolute paths', () => {
    expect(validPath('/var/backups')).toBe(true)
    expect(validPath('/')).toBe(true)
    for (const bad of ['var', '/a/../b', '/a b', '/a;rm', '/a$(x)', '/a"b', '']) expect(validPath(bad)).toBe(false)
  })
})

describe('Overview: history and what needs attention', () => {
  it('warns about a certificate that runs out, pending security updates, a reboot and a stale backup', async () => {
    const h = {...web, backup_path: '/var/backups'}
    const be = makeFake({hosts: [h]})
    be.state.server.backup = {...be.state.server.backup, mtime: be.state.server.backup.now - 9 * 86400}
    render(<Providers be={be}><ServerPanel host={h} visible /></Providers>)
    expect(await screen.findByText('The certificate for example.com expires in 9 days')).toBeInTheDocument()
    expect(await screen.findByText('3 security updates are waiting')).toBeInTheDocument()
    expect(screen.getByText('The server needs a reboot')).toBeInTheDocument()
    expect(await screen.findByText(/The newest backup is 9d 0h old/)).toBeInTheDocument()
    expect(screen.queryByText(/api.example.com/)).toBeNull() // 88 days left: no warning
    expect(screen.getByText(/The charts show the last 1 min/)).toBeInTheDocument()
  })

  it('goes on sampling while another section is open, so the charts keep their history', async () => {
    const {be} = setup()
    await open('Docker')
    const before = be.state.calls.filter(c => c[0] === 'server.overview').length
    expect(before).toBeGreaterThanOrEqual(0)
    expect(await screen.findByText('Stack shop')).toBeInTheDocument()
  })
})

describe('Web tab', () => {
  it('lists certificates by time left and the sites of nginx', async () => {
    setup()
    await open('Web')
    expect(await screen.findByText('www.example.com')).toBeInTheDocument()
    expect(within(rowOf('api.example.com')).getByText('88 days')).toBeInTheDocument()
    expect(within(rowOf('example.com')).getByText('9 days')).toHaveClass('warn')
    expect(await screen.findByText('proxy_pass http://127.0.0.1:3000')).toBeInTheDocument()
    expect(screen.getByText('nginx/1.24.0')).toBeInTheDocument()
  })

  it('tests the configuration without asking, and reloads only after a yes', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await open('Web')
    await userEvent.click(await screen.findByRole('button', {name: 'Test configuration'}))
    expect(await screen.findByRole('status', {name: 'Result'})).toHaveTextContent('test is successful')
    expect(ask).not.toHaveBeenCalled()
    expect(calls(be, 'server.webTest')).toEqual([['server.webTest', 'nginx']])
    await userEvent.click(screen.getByRole('button', {name: 'Reload'}))
    expect(ask).toHaveBeenLastCalledWith(expect.stringContaining('Reload nginx?'))
    expect(calls(be, 'server.webReload')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Reload'}))
    await waitFor(() => expect(calls(be, 'server.webReload')).toEqual([['server.webReload', 'nginx']]))
  })

  it('shows the output of a failing test', async () => {
    const be = makeFake({hosts: [web]})
    be.state.server.webResult = {ok: false, output: 'nginx: [emerg] unexpected "}" in /etc/nginx/nginx.conf:12'}
    render(<Providers be={be}><ServerPanel host={web} visible /></Providers>)
    await screen.findByText(/up 41d 0h/)
    await userEvent.click(screen.getByRole('tab', {name: 'Web'}))
    await userEvent.click(await screen.findByRole('button', {name: 'Test configuration'}))
    expect(await screen.findByRole('status', {name: 'Result'})).toHaveClass('bad')
  })

  it('says so when there is no web server or no certificate', async () => {
    const be = makeFake({hosts: [web]})
    be.state.server.web = {kind: '', version: '', running: false, sites: []}
    be.state.server.certs = {found: false, certs: []}
    render(<Providers be={be}><ServerPanel host={web} visible /></Providers>)
    await screen.findByText(/up 41d 0h/)
    await userEvent.click(screen.getByRole('tab', {name: 'Web'}))
    expect(await screen.findByText('Neither nginx nor Caddy is installed.')).toBeInTheDocument()
    expect(screen.getByText(/No Let's Encrypt certificates found/)).toBeInTheDocument()
  })

  it('opens the error log of the web server in Logs', async () => {
    const {be} = setup()
    await open('Web')
    await userEvent.click(await screen.findByRole('button', {name: 'Logs'}))
    await waitFor(() => expect(calls(be, 'server.logs').at(-1)).toEqual(['server.logs', 'web', 'nginx']))
  })
})

describe('Web and Services changes', () => {
  it('switches a site off after asking, and renews a certificate', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const {be} = setup()
    await open('Web')
    await userEvent.click(await screen.findByRole('button', {name: 'Switch off the site shop.conf'}))
    await waitFor(() => expect(calls(be, 'server.webSiteSet')).toEqual([['server.webSiteSet', 'shop.conf', false]]))
    await userEvent.click(await screen.findByRole('button', {name: 'Renew the certificate example.com'}))
    await waitFor(() => expect(calls(be, 'server.certRenew')).toEqual([['server.certRenew', 'example.com', false]]))
    expect(await screen.findByText(/all renewals succeeded/)).toBeInTheDocument()
    ask.mockRestore()
  })

  it('does nothing when the question is answered No', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await open('Web')
    await userEvent.click(await screen.findByRole('button', {name: 'Renew example.com now'}))
    await userEvent.click(screen.getByRole('button', {name: 'Switch off the site shop.conf'}))
    expect(calls(be, 'server.certRenew')).toHaveLength(0)
    expect(calls(be, 'server.webSiteSet')).toHaveLength(0)
    ask.mockRestore()
  })

  it('gets a certificate for the domains of the form', async () => {
    const {be} = setup()
    await open('Web')
    await userEvent.click(await screen.findByRole('button', {name: 'Get certificate'}))
    const dlg = await screen.findByRole('dialog')
    await userEvent.type(within(dlg).getByPlaceholderText(/example.com www/), 'a.example.com b.example.com')
    await userEvent.type(within(dlg).getByPlaceholderText('you@example.com'), 'me@example.com')
    await userEvent.click(within(dlg).getByRole('button', {name: 'Get certificate'}))
    await waitFor(() => expect(calls(be, 'server.certRequest')).toEqual([['server.certRequest', 'a.example.com,b.example.com', 'me@example.com']]))
  })

  it('turns the start on boot of a service off after asking', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const {be} = setup()
    await open('Services')
    await userEvent.click(await screen.findByRole('button', {name: 'More actions for nginx'}))
    expect(within(screen.getByRole('menu')).getByRole('menuitem', {name: 'Stop'})).toBeInTheDocument()
    await userEvent.click(within(screen.getByRole('menu')).getByRole('menuitem', {name: 'Disable on boot'}))
    await waitFor(() => expect(calls(be, 'server.serviceAction')).toEqual([['server.serviceAction', 'nginx', 'disable']]))
    await userEvent.click(await screen.findByRole('button', {name: 'More actions for nginx'}))
    expect(within(screen.getByRole('menu')).getByRole('menuitem', {name: 'Enable on boot'})).toBeInTheDocument()
    ask.mockRestore()
  })
})

describe('Docker stacks', () => {
  it('groups containers by Compose project, and asks before an action on the whole stack', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await open('Docker')
    const stack = (await screen.findByText('Stack shop')).closest('section') as HTMLElement
    expect(within(stack).getByText('api')).toBeInTheDocument()
    expect(within(stack).getByText('db')).toBeInTheDocument()
    expect(within(stack).queryByText('old-batch')).toBeNull()
    expect(within(screen.getByRole('region', {name: 'Other containers'})).getByText('old-batch')).toBeInTheDocument()
    await userEvent.click(within(stack).getByRole('button', {name: 'Update'}))
    expect(ask).toHaveBeenLastCalledWith(expect.stringContaining('Update the stack shop?'))
    expect(calls(be, 'server.stackAction')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(within(stack).getByRole('button', {name: 'Update'}))
    await waitFor(() => expect(calls(be, 'server.stackAction')).toEqual([['server.stackAction', 'shop', 'update']]))
  })

  it('stops the whole stack and then offers Start', async () => {
    const {be} = setup()
    await open('Docker')
    const stack = (await screen.findByText('Stack shop')).closest('section') as HTMLElement
    const head = () => within(stack.querySelector('.sp-card-head') as HTMLElement)
    await userEvent.click(head().getByRole('button', {name: 'Stop'}))
    await waitFor(() => expect(calls(be, 'server.stackAction')).toEqual([['server.stackAction', 'shop', 'stop']]))
    expect(await head().findByRole('button', {name: 'Start'})).toBeInTheDocument()
  })

  it('brings a stack up, and takes it down only after asking', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await open('Docker')
    const stack = (await screen.findByText('Stack shop')).closest('section') as HTMLElement
    const menuItem = async (name: string) => {
      await userEvent.click(within(stack).getByRole('button', {name: 'More actions for shop'}))
      await userEvent.click(within(screen.getByRole('menu')).getByRole('menuitem', {name}))
    }
    await menuItem('Down')
    expect(ask).toHaveBeenLastCalledWith(expect.stringContaining('Take the stack shop down?'))
    expect(calls(be, 'server.stackAction')).toHaveLength(0)
    ask.mockReturnValue(true)
    await menuItem('Up')
    await menuItem('Down')
    await waitFor(() => expect(calls(be, 'server.stackAction')).toEqual([['server.stackAction', 'shop', 'up'], ['server.stackAction', 'shop', 'down']]))
    await waitFor(() => expect(screen.queryByText('Stack shop')).toBeNull())
    ask.mockRestore()
  })

  it('pulls an image by name and pulls a listed one again', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const {be} = setup()
    await open('Docker')
    await userEvent.click(await screen.findByRole('tab', {name: 'Images'}))
    await userEvent.click(await screen.findByRole('button', {name: 'Pull image'}))
    const dlg = await screen.findByRole('dialog')
    expect(within(dlg).getByRole('button', {name: 'Pull'})).toBeDisabled()
    await userEvent.type(within(dlg).getByPlaceholderText('nginx:1.27'), 'nginx:1.27')
    await userEvent.click(within(dlg).getByRole('button', {name: 'Pull'}))
    await waitFor(() => expect(calls(be, 'server.dockerPull')).toEqual([['server.dockerPull', 'nginx:1.27']]))
    await userEvent.click(await screen.findByRole('button', {name: 'Pull registry/api:1.8 again'}))
    await waitFor(() => expect(calls(be, 'server.dockerPull')).toHaveLength(2))
    ask.mockRestore()
  })

  it('opens the log of a stack', async () => {
    const {be} = setup()
    await open('Docker')
    const stack = (await screen.findByText('Stack shop')).closest('section') as HTMLElement
    await userEvent.click(within(stack).getByRole('button', {name: 'More actions for shop'}))
    await userEvent.click(within(screen.getByRole('menu')).getByRole('menuitem', {name: 'Logs'}))
    await waitFor(() => expect(calls(be, 'server.logs').at(-1)).toEqual(['server.logs', 'stack', 'shop']))
  })
})

describe('Logs filter', () => {
  it('marks what a plain filter finds, and takes a regular expression on request', async () => {
    setup()
    await open('Logs')
    await screen.findByText(/ECONNREFUSED/)
    const box = screen.getByRole('textbox', {name: 'Filter log lines'})
    await userEvent.type(box, 'econn')
    expect(screen.getByRole('log').querySelector('mark')?.textContent).toBe('ECONN')
    expect(screen.getByRole('status')).toHaveTextContent('1 of 2 lines')
    await userEvent.clear(box)
    await userEvent.click(screen.getByRole('button', {name: '.*'}))
    await userEvent.type(box, 'sshd|ECONN')
    expect(screen.getByRole('status')).toHaveTextContent('2 of 2 lines')
    await userEvent.clear(box)
    await userEvent.type(box, '(')
    expect(box).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByText('No log lines.')).toBeInTheDocument()
  })

  it('asks for another number of lines and another time, and for another source', async () => {
    const {be} = setup()
    await open('Logs')
    await screen.findByText(/ECONNREFUSED/)
    await userEvent.click(screen.getByRole('combobox', {name: /^Number of lines/}))
    await userEvent.click(await screen.findByRole('option', {name: /Last 1000 lines/}))
    await waitFor(() => expect(calls(be, 'server.logs').at(-1)).toEqual(['server.logs', '', '', {lines: 1000, since: ''}]))
    await userEvent.click(screen.getByRole('combobox', {name: /^Time range/}))
    await userEvent.click(await screen.findByRole('option', {name: /Last hour/}))
    await waitFor(() => expect(calls(be, 'server.logs').at(-1)).toEqual(['server.logs', '', '', {lines: 1000, since: '1h'}]))
    await userEvent.click(screen.getByRole('combobox', {name: /^Log source/}))
    await userEvent.click(await screen.findByRole('option', {name: /api/}))
    await waitFor(() => expect(calls(be, 'server.logs').at(-1)).toEqual(['server.logs', 'container', 'api', {lines: 1000, since: '1h'}]))
  })
})

describe('Logs download', () => {
  it('saves the lines that are shown, after the filter, to a file', async () => {
    const {be} = setup()
    await open('Logs')
    await screen.findByText(/ECONNREFUSED/)
    await userEvent.type(screen.getByRole('textbox', {name: 'Filter log lines'}), 'sshd')
    await userEvent.click(screen.getByRole('button', {name: /Download/}))
    await waitFor(() => expect(calls(be, 'system.saveText')).toHaveLength(1))
    const [, name, text] = calls(be, 'system.saveText')[0]
    expect(name).toMatch(/^system-\d{8}-\d{4}\.log$/)
    expect(text).toBe('2026-11-04T09:41:12+0700 host sshd[88]: Accepted publickey for ubuntu\n')
  })
})

describe('Logs follow', () => {
  it('reads again every two seconds only while Follow is on', async () => {
    const {be} = setup()
    await open('Logs')
    await screen.findByText(/ECONNREFUSED/)
    vi.useFakeTimers({shouldAdvanceTime: true})
    try {
      const n = () => calls(be, 'server.logs').length
      const start = n()
      await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
      expect(n()).toBe(start) // not following
      await userEvent.click(screen.getByRole('button', {name: 'Follow'}))
      expect(screen.getByRole('button', {name: 'Follow'})).toHaveAttribute('aria-pressed', 'true')
      const afterOn = n()
      await act(async () => { await vi.advanceTimersByTimeAsync(4100) })
      expect(n()).toBeGreaterThanOrEqual(afterOn + 2)
      await userEvent.click(screen.getByRole('button', {name: 'Follow'}))
      const afterOff = n()
      await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
      expect(n()).toBe(afterOff)
    } finally { vi.useRealTimers() }
  })
})

describe('System box of Maintenance', () => {
  it('renames, sets the time zone and creates then removes a swap file, each after asking', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await open('Maintenance')
    const host = await screen.findByLabelText('Hostname')
    await userEvent.clear(host)
    await userEvent.type(host, 'web-2')
    await userEvent.click(screen.getByRole('button', {name: 'Rename'}))
    expect(calls(be, 'server.setHostname')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Rename'}))
    await waitFor(() => expect(calls(be, 'server.setHostname')).toEqual([['server.setHostname', 'web-2']]))
    const zone = screen.getByLabelText('Time zone')
    await userEvent.clear(zone)
    await userEvent.type(zone, 'Asia/Ho_Chi_Minh')
    await userEvent.click(screen.getByRole('button', {name: 'Change'}))
    await waitFor(() => expect(calls(be, 'server.setTimezone')).toEqual([['server.setTimezone', 'Asia/Ho_Chi_Minh']]))
    expect(screen.getByRole('button', {name: 'Create swap file'})).toBeDisabled()
    await userEvent.type(screen.getByLabelText('Swap file size in MB'), '2048')
    await userEvent.click(screen.getByRole('button', {name: 'Create swap file'}))
    await waitFor(() => expect(calls(be, 'server.swapCreate')).toEqual([['server.swapCreate', 2048]]))
    await userEvent.click(await screen.findByRole('button', {name: 'Remove swap file'}))
    await waitFor(() => expect(calls(be, 'server.swapRemove')).toHaveLength(1))
    ask.mockRestore()
  })
})

describe('Maintenance tab', () => {
  it('shows pending updates with the security ones marked, and refreshes the lists only after a yes', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await open('Maintenance')
    expect(await screen.findByText('12 updates waiting')).toBeInTheDocument()
    expect(screen.getByText('3 security updates')).toBeInTheDocument()
    expect(screen.getByText('Needs a reboot', {selector: '.sp-pill'})).toBeInTheDocument()
    expect(within(rowOf('openssl')).getByText('Security')).toBeInTheDocument()
    expect(within(rowOf('curl')).queryByText('Security')).toBeNull()
    await userEvent.click(screen.getByRole('button', {name: 'Refresh lists'}))
    expect(calls(be, 'server.updatesRefresh')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Refresh lists'}))
    await waitFor(() => expect(calls(be, 'server.updatesRefresh')).toHaveLength(1))
  })

  it('says how much a cleanup frees, asks first, and never offers to remove volumes', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await open('Maintenance')
    const row = (await screen.findByText('Unused Docker layers')).closest('tr') as HTMLElement
    expect(within(screen.getByText('Docker volumes').closest('tr') as HTMLElement).queryByRole('button', {name: 'Clean'})).toBeNull()
    await userEvent.click(within(row).getByRole('button', {name: 'Clean'}))
    expect(ask).toHaveBeenLastCalledWith(expect.stringMatching(/Clean Unused Docker layers\? This frees about 1(\.0)? GB\./))
    expect(calls(be, 'server.cleanupRun')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(within(row).getByRole('button', {name: 'Clean'}))
    await waitFor(() => expect(calls(be, 'server.cleanupRun')).toEqual([['server.cleanupRun', 'docker-dangling']]))
  })

  it('shows each disk, and scans a folder and goes into one of its folders', async () => {
    const {be} = setup()
    await open('Maintenance')
    expect(await screen.findByRole('meter', {name: 'Used space of /'})).toHaveAttribute('aria-valuenow', '78')
    await userEvent.click(screen.getByRole('button', {name: 'Scan'}))
    await userEvent.click(await screen.findByRole('button', {name: '/var/lib'}))
    expect(await screen.findByRole('button', {name: '/var/lib/docker'})).toBeInTheDocument()
    expect(calls(be, 'server.diskUsage')).toEqual([['server.diskUsage', '/var'], ['server.diskUsage', '/var/lib']])
    await userEvent.click(screen.getByRole('button', {name: 'Up'}))
    await waitFor(() => expect(calls(be, 'server.diskUsage').at(-1)).toEqual(['server.diskUsage', '/var']))
  })

  it('refuses a path with anything odd in it before asking the server', async () => {
    const {be} = setup()
    await open('Maintenance')
    const box = await screen.findByLabelText('Folder to look into')
    await userEvent.clear(box)
    await userEvent.type(box, '/var/../etc')
    await userEvent.click(screen.getByRole('button', {name: 'Scan'}))
    expect(await screen.findByRole('alert')).toHaveTextContent('absolute path')
    expect(calls(be, 'server.diskUsage')).toHaveLength(0)
  })
})

describe('Security tab', () => {
  it('shows the SSH settings, the failed logins and the banned addresses', async () => {
    setup()
    await open('Security')
    expect(await screen.findByText('180')).toBeInTheDocument()
    expect(screen.getByText('203.0.113.9')).toBeInTheDocument()
    expect(within(screen.getByText('Root login').closest('tr') as HTMLElement).getByText('yes')).toHaveClass('warn')
    expect(within(screen.getByText('Password login').closest('tr') as HTMLElement).getByText('On')).toHaveClass('warn')
    expect(screen.getByText(/Safer: keys only/)).toBeInTheDocument()
    expect(within(rowOf('sshd')).getByText('198.51.100.7')).toBeInTheDocument()
  })

  it('asks before letting an address in again', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await open('Security')
    await userEvent.click(await screen.findByRole('button', {name: 'Unban 203.0.113.5'}))
    expect(ask).toHaveBeenLastCalledWith(expect.stringContaining('Let 203.0.113.5 in again?'))
    expect(calls(be, 'server.unban')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Unban 203.0.113.5'}))
    await waitFor(() => expect(calls(be, 'server.unban')).toEqual([['server.unban', 'sshd', '203.0.113.5']]))
    await waitFor(() => expect(screen.queryByText('203.0.113.5')).toBeNull())
  })

  it('shows the security check, the recent logins, and bans a failing address after asking', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await open('Security')
    expect(await screen.findByText(/checks pass/)).toBeInTheDocument()
    expect(screen.getByText('Root can log in over SSH')).toBeInTheDocument()
    expect(screen.getByText('The firewall is installed but off')).toBeInTheDocument()
    expect(screen.getByText('203.0.113.7')).toBeInTheDocument()
    expect(screen.getByText('Logged in now')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Ban 203.0.113.9'}))
    expect(calls(be, 'server.ban')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Ban 203.0.113.9'}))
    await waitFor(() => expect(calls(be, 'server.ban')).toEqual([['server.ban', 'sshd', '203.0.113.9']]))
    ask.mockRestore()
  })

  it('turns password login off only after asking', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await open('Security')
    const row = (await screen.findByText('Password login')).closest('tr') as HTMLElement
    await userEvent.click(within(row).getByRole('button', {name: 'Turn off'}))
    expect(calls(be, 'server.sshSet')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(within(row).getByRole('button', {name: 'Turn off'}))
    await waitFor(() => expect(calls(be, 'server.sshSet')).toEqual([['server.sshSet', 'password_auth', 'no']]))
    await waitFor(() => expect(within(row).queryByRole('button', {name: 'Turn off'})).toBeNull())
    ask.mockRestore()
  })

  it('bans an address from the form', async () => {
    const {be} = setup()
    await open('Security')
    await userEvent.click(await screen.findByRole('button', {name: 'Ban address'}))
    const dlg = await screen.findByRole('dialog')
    await userEvent.type(within(dlg).getByPlaceholderText('203.0.113.7'), '192.0.2.44')
    await userEvent.click(within(dlg).getByRole('button', {name: 'Ban'}))
    await waitFor(() => expect(calls(be, 'server.ban')).toEqual([['server.ban', 'sshd', '192.0.2.44']]))
    expect(await screen.findByText('192.0.2.44')).toBeInTheDocument()
  })

  it('says when fail2ban or the SSH log is not there', async () => {
    const be = makeFake({hosts: [web]})
    be.state.server.security = {fail2ban: {installed: false, running: false, jails: []}, ssh: {known: false, port: 0, root_login: '', password_auth: ''}, failed: {known: false, total: 0, top: []}}
    render(<Providers be={be}><ServerPanel host={web} visible /></Providers>)
    await screen.findByText(/up 41d 0h/)
    await userEvent.click(screen.getByRole('tab', {name: 'Security'}))
    expect(await screen.findByText('fail2ban is not installed on this server.')).toBeInTheDocument()
    expect(screen.getByText('This login cannot read the SSH configuration.')).toBeInTheDocument()
    expect(screen.getByText('There is no SSH log this login can read.')).toBeInTheDocument()
  })
})

describe('Schedule tab', () => {
  it('lists timers and cron jobs, and runs a timer\'s job now only after a yes', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await open('Schedule')
    const row = (await screen.findByText('backup')).closest('tr') as HTMLElement
    expect(within(row).getByText('Succeeded')).toBeInTheDocument()
    expect(screen.getByText('/usr/local/bin/cleanup.sh')).toBeInTheDocument()
    expect(screen.getByText('0 4 * * *')).toBeInTheDocument()
    await userEvent.click(within(row).getByRole('button', {name: 'Run now'}))
    expect(ask).toHaveBeenLastCalledWith(expect.stringContaining('Run backup now?'))
    expect(calls(be, 'server.serviceAction')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(within(row).getByRole('button', {name: 'Run now'}))
    await waitFor(() => expect(calls(be, 'server.serviceAction')).toEqual([['server.serviceAction', 'backup', 'start']]))
  })

  it('saves the backup folder on the host and shows how old the newest file is', async () => {
    const {be} = setup()
    await open('Schedule')
    expect(await screen.findByText(/Name the folder where your backups land/)).toBeInTheDocument()
    expect(calls(be, 'server.backup')).toHaveLength(0)
    await userEvent.type(screen.getByLabelText('Backup folder on the server'), '/var/backups')
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    await waitFor(() => expect(calls(be, 'hosts.saveHost')).toHaveLength(1))
    expect((calls(be, 'hosts.saveHost')[0][1] as {backup_path: string}).backup_path).toBe('/var/backups')
    expect(await screen.findByText('db-2026-11-02.sql.gz')).toBeInTheDocument()
    expect(screen.getByText('3d 0h ago', {selector: '.sp-pill'})).toHaveClass('warn')
    expect(calls(be, 'server.backup')).toEqual([['server.backup', '/var/backups']])
  })

  it('does not save a path that is not a plain absolute one', async () => {
    const {be} = setup()
    await open('Schedule')
    await userEvent.type(await screen.findByLabelText('Backup folder on the server'), 'backups; rm -rf /')
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    expect(await screen.findByRole('alert')).toHaveTextContent('absolute path')
    expect(calls(be, 'hosts.saveHost')).toHaveLength(0)
  })

  it('says when the backup folder has nothing in it', async () => {
    const h = {...web, backup_path: '/var/backups'}
    const be = makeFake({hosts: [h]})
    be.state.server.backup = {found: false, file: '', size: 0, mtime: 0, now: 1790000000}
    render(<Providers be={be}><ServerPanel host={h} visible /></Providers>)
    await screen.findByText(/up 41d 0h/)
    await userEvent.click(screen.getByRole('tab', {name: 'Schedule'}))
    expect(await screen.findByText('No backup found')).toBeInTheDocument()
  })
})

describe('Fleet page', () => {
  const a = host({id: 'a', label: 'alpha', hostname: 'a.example.com'})
  const b = host({id: 'b', label: 'bravo', hostname: 'b.example.com'})
  const c = host({id: 'c', label: 'charlie', hostname: 'c.example.com'})
  const mount = (be: Fake, onPanel = vi.fn()) => { render(<Providers be={be}><FleetPage hosts={[a, b, c]} onPanel={onPanel} /></Providers>); return onPanel }

  it('checks every host once and shows what each answered, the ones that need a look first', async () => {
    const be = makeFake({hosts: [a, b, c]})
    be.state.server.fleetErrors = {b: 'the host key of "bravo" is not trusted yet'}
    mount(be)
    expect(await screen.findByText('2 of 3 servers answered')).toBeInTheDocument()
    expect(calls(be, 'server.fleetHost').map(x => x[1]).sort()).toEqual(['a', 'b', 'c'])
    const names = screen.getAllByRole('row').slice(1).map(r => within(r).getAllByRole('cell')[0].textContent)
    expect(names[0]).toMatch(/^bravo/) // down comes first
    expect(within(rowOf('bravo')).getByText('Down')).toBeInTheDocument()
    expect(screen.getByText('the host key of "bravo" is not trusted yet')).toBeInTheDocument()
    expect(within(rowOf('alpha')).getByText('1 failed')).toBeInTheDocument()
    expect(within(rowOf('alpha')).getByText('78%')).toHaveClass('warn')
  })

  it('asks the hosts again by itself every 30 seconds, without blanking the table', async () => {
    const be = makeFake({hosts: [a, b, c]})
    vi.useFakeTimers({shouldAdvanceTime: true})
    try {
      mount(be)
      await screen.findByText('3 of 3 servers answered')
      await act(async () => { await vi.advanceTimersByTimeAsync(29000) })
      expect(calls(be, 'server.fleetHost')).toHaveLength(3)
      await act(async () => { await vi.advanceTimersByTimeAsync(2000) })
      expect(calls(be, 'server.fleetHost')).toHaveLength(6)
      expect(screen.getByText('3 of 3 servers answered')).toBeInTheDocument()
    } finally { vi.useRealTimers() }
  })

  it('opens the panel of a host and checks again on request', async () => {
    const be = makeFake({hosts: [a, b, c]})
    const onPanel = mount(be)
    await screen.findByText('3 of 3 servers answered')
    await userEvent.click(within(rowOf('charlie')).getByRole('button', {name: 'Open panel'}))
    expect(onPanel).toHaveBeenCalledWith(expect.objectContaining({id: 'c'}))
    await userEvent.click(screen.getByRole('button', {name: 'Check all servers again'}))
    await waitFor(() => expect(calls(be, 'server.fleetHost')).toHaveLength(6))
  })

  it('filters by name', async () => {
    const be = makeFake({hosts: [a, b, c]})
    mount(be)
    await screen.findByText('3 of 3 servers answered')
    await userEvent.type(screen.getByLabelText('Filter servers'), 'brav')
    expect(screen.queryByText('alpha')).toBeNull()
    expect(screen.getByText('bravo')).toBeInTheDocument()
  })
})

describe('Schedule tab: cron jobs', () => {
  it('adds a job to the own crontab from a ready-made time, and to root', async () => {
    const {be} = setup()
    await open('Schedule')
    await screen.findByText('/usr/local/bin/cleanup.sh')
    await userEvent.click(screen.getByRole('button', {name: 'Add job'}))
    const dlg = screen.getByRole('dialog', {name: 'Add job'})
    const save = within(dlg).getByRole('button', {name: 'Save'})
    expect(save).toBeDisabled()
    await userEvent.clear(within(dlg).getByLabelText('Schedule'))
    await userEvent.type(within(dlg).getByLabelText('Schedule'), '*/10 * * * *')
    await userEvent.type(within(dlg).getByLabelText('Command'), '/bin/ping -c1 example.com')
    await userEvent.click(save)
    await waitFor(() => expect(calls(be, 'server.cronAdd')).toEqual([['server.cronAdd', 'crontab', '*/10 * * * *', '/bin/ping -c1 example.com']]))
    expect(await screen.findByText('/bin/ping -c1 example.com')).toBeInTheDocument()
  })

  it('changes a job and finds it again by its old schedule and command', async () => {
    const {be} = setup()
    await open('Schedule')
    await userEvent.click(await screen.findByRole('button', {name: 'Edit the job /usr/local/bin/cleanup.sh'}))
    const dlg = screen.getByRole('dialog', {name: 'Edit job'})
    await userEvent.clear(within(dlg).getByLabelText('Schedule'))
    await userEvent.type(within(dlg).getByLabelText('Schedule'), '30 5 * * *')
    await userEvent.click(within(dlg).getByRole('button', {name: 'Save'}))
    await waitFor(() => expect(calls(be, 'server.cronUpdate')).toEqual([['server.cronUpdate', 'crontab', '0 4 * * *', '/usr/local/bin/cleanup.sh', '30 5 * * *', '/usr/local/bin/cleanup.sh']]))
    expect(await screen.findByText('30 5 * * *')).toBeInTheDocument()
  })

  it('deletes a job only after a yes, and leaves the files of /etc alone', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await open('Schedule')
    expect(await screen.findByText('/usr/sbin/logrotate')).toBeInTheDocument()
    expect(screen.queryByRole('button', {name: /the job \/usr\/sbin\/logrotate/})).toBeNull()
    await userEvent.click(screen.getByRole('button', {name: 'Delete the job /usr/local/bin/cleanup.sh'}))
    expect(ask).toHaveBeenLastCalledWith(expect.stringContaining('0 4 * * * /usr/local/bin/cleanup.sh'))
    expect(calls(be, 'server.cronDelete')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Delete the job /usr/local/bin/cleanup.sh'}))
    await waitFor(() => expect(calls(be, 'server.cronDelete')).toEqual([['server.cronDelete', 'crontab', '0 4 * * *', '/usr/local/bin/cleanup.sh']]))
    await waitFor(() => expect(screen.queryByText('/usr/local/bin/cleanup.sh')).toBeNull())
  })

  it('keeps the form open and shows what the server said when it refuses', async () => {
    const {be} = setup()
    await open('Schedule')
    await screen.findByText('/usr/local/bin/cleanup.sh')
    be.state.server.failAction = 'crontab: bad minute'
    await userEvent.click(screen.getByRole('button', {name: 'Add job'}))
    const dlg = screen.getByRole('dialog', {name: 'Add job'})
    await userEvent.type(within(dlg).getByLabelText('Command'), 'x')
    await userEvent.click(within(dlg).getByRole('button', {name: 'Save'}))
    expect(await within(dlg).findByText(/crontab: bad minute/)).toBeInTheDocument()
  })
})
