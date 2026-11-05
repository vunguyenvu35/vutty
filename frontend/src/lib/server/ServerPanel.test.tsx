import {render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {ServerPanel} from './ServerPanel'
import {host, makeFake, Providers} from '../../test/fakeBackend'

type Fake = ReturnType<typeof makeFake>
const web = host({id: 'h1', label: 'web-1', hostname: 'w1.example.com', username: 'ubuntu', port: 22})
const calls = (be: Fake, n: string) => be.state.calls.filter(c => c[0] === n)

function setup() {
  const be = makeFake({hosts: [web]})
  const view = render(<Providers be={be}><ServerPanel host={web} visible /></Providers>)
  return {be, ...view}
}
const open = (name: string) => userEvent.click(screen.getByRole('tab', {name}))

describe('Server panel (tab)', () => {
  it('shows who it is about and the overview, with what needs attention', async () => {
    setup()
    expect(screen.getByRole('heading', {name: 'web-1'})).toBeInTheDocument()
    expect(screen.getByText('ubuntu@w1.example.com')).toBeInTheDocument()
    expect(await screen.findByText(/up 41d 0h/)).toBeInTheDocument()
    expect(screen.getByRole('meter', {name: 'Disk /'})).toHaveAttribute('aria-valuenow', '78')
    expect(await screen.findByText('app-worker has failed')).toBeInTheDocument() // from the failed service
    expect(screen.getByText('The disk is 78% full')).toBeInTheDocument()
    await userEvent.click(within(screen.getByText('app-worker has failed').closest('.sp-attn') as HTMLElement).getByRole('button', {name: 'Open'}))
    expect(await screen.findByRole('columnheader', {name: 'Startup'})).toBeInTheDocument() // went to Services
  })

  it('asks in the row before ending a process', async () => {
    const {be} = setup()
    await screen.findByText(/up 41d 0h/)
    await open('Processes')
    const row = (await screen.findByText('node server.js')).closest('tr') as HTMLElement
    await userEvent.click(within(row).getByRole('button', {name: /End process 2210/}))
    expect(calls(be, 'server.kill')).toHaveLength(0)
    await userEvent.click(within(row).getByRole('button', {name: 'Force kill'}))
    await waitFor(() => expect(calls(be, 'server.kill')).toEqual([['server.kill', 2210, 'KILL']]))
  })

  it('shows the firewall state of each port and offers no way to change it', async () => {
    const {be} = setup()
    await screen.findByText(/up 41d 0h/)
    await open('Ports & firewall')
    expect(await screen.findByText('Firewall (ufw) is on')).toBeInTheDocument()
    const minio = screen.getByText('minio').closest('tr') as HTMLElement
    expect(within(minio).getByText('Shut', {selector: '.sp-pill'})).toBeInTheDocument()
    expect(within(screen.getByText('sshd').closest('tr') as HTMLElement).getByText('Allowed')).toBeInTheDocument()
    expect(screen.queryByText(/Open to the outside/)).toBeNull() // minio listens everywhere but the firewall keeps it out
    expect(screen.getByText(/only shown here/)).toBeInTheDocument()
    expect(within(screen.getByRole('region')).queryAllByRole('button').filter(x => /^(Allow|Shut)$/.test(x.textContent ?? ''))).toHaveLength(0)
    expect(calls(be, 'server.firewallRule')).toHaveLength(0)
  })

  it('asks before starting, restarting or stopping a service or a container, and does nothing on No', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await screen.findByText(/up 41d 0h/)
    await open('Services')
    const nginx = (await screen.findByText('nginx')).closest('tr') as HTMLElement
    await userEvent.click(within(nginx).getByRole('button', {name: 'Restart'}))
    expect(ask).toHaveBeenLastCalledWith(expect.stringContaining('Restart the service nginx'))
    const worker = screen.getByText('app-worker').closest('tr') as HTMLElement
    await userEvent.click(within(worker).getByRole('button', {name: 'Start'}))
    expect(ask).toHaveBeenLastCalledWith(expect.stringContaining('Start the service app-worker'))
    expect(calls(be, 'server.serviceAction')).toHaveLength(0)
    await open('Docker')
    const api = (await screen.findByText('api')).closest('tr') as HTMLElement
    await userEvent.click(within(api).getByRole('button', {name: 'Restart'}))
    expect(ask).toHaveBeenLastCalledWith(expect.stringContaining('Restart the container api'))
    expect(calls(be, 'server.containerAction')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(within(api).getByRole('button', {name: 'Restart'}))
    await waitFor(() => expect(calls(be, 'server.containerAction')).toEqual([['server.containerAction', 'api', 'restart']]))
  })
})

describe('Server panel: Docker', () => {
  it('opens a shell in a container, shows its load, and keeps the environment hidden until asked', async () => {
    const be = makeFake({hosts: [web]})
    const onShell = vi.fn()
    render(<Providers be={be}><ServerPanel host={web} visible onShell={onShell} /></Providers>)
    await screen.findByRole('tab', {name: 'Docker'})
    await open('Docker')
    const api = (await screen.findByText('api')).closest('tr') as HTMLElement
    expect(await within(api).findByText('12.0%')).toBeInTheDocument() // CPU from docker stats
    await userEvent.click(within(api).getByRole('button', {name: 'Shell'}))
    await waitFor(() => expect(onShell).toHaveBeenCalledWith(web, 'docker exec -it api sh', 'web-1 · api'))
    const stopped = screen.getByText('old-batch').closest('tr') as HTMLElement
    expect(within(stopped).queryByRole('button', {name: 'Shell'})).toBeNull() // only a running container has a shell
    await userEvent.click(within(api).getByRole('button', {name: 'Details of api'}))
    expect(await screen.findByText('DB_PASSWORD')).toBeInTheDocument()
    expect(screen.queryByText('hunter2')).toBeNull()
    await userEvent.click(screen.getByRole('button', {name: 'Show DB_PASSWORD'}))
    expect(screen.getByText('hunter2')).toBeInTheDocument()
  })

  it('lists images, volumes and networks, and deletes only what is not in use, after asking', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const be = makeFake({hosts: [web]})
    render(<Providers be={be}><ServerPanel host={web} visible /></Providers>)
    await screen.findByRole('tab', {name: 'Docker'})
    await open('Docker')
    await userEvent.click(await screen.findByRole('tab', {name: 'Images'}))
    const used = (await screen.findByText('registry/api:1.8')).closest('tr') as HTMLElement
    expect(within(used).getByText('In use')).toBeInTheDocument()
    expect(within(used).queryByRole('button', {name: 'Delete'})).toBeNull()
    const loose = screen.getByText('<none>').closest('tr') as HTMLElement
    await userEvent.click(within(loose).getByRole('button', {name: 'Delete'}))
    await waitFor(() => expect(calls(be, 'server.dockerRemove')).toEqual([['server.dockerRemove', 'image', 'cc33dd44']]))
    await userEvent.click(screen.getByRole('tab', {name: 'Volumes'}))
    const old = (await screen.findByText('old-cache')).closest('tr') as HTMLElement
    await userEvent.click(within(old).getByRole('button', {name: 'Delete'}))
    await waitFor(() => expect(calls(be, 'server.dockerRemove')).toHaveLength(2))
    await userEvent.click(screen.getByRole('tab', {name: 'Networks'}))
    expect(await screen.findByText('shop_default')).toBeInTheDocument()
  })
})

describe('Server panel: history', () => {
  it('shows the last half hour at first, and a longer range from what this computer kept', async () => {
    localStorage.clear()
    const now = Date.now()
    localStorage.setItem('vutty.panel.history.h1', JSON.stringify([{t: Math.floor((now - 3 * 3600_000) / 60000) * 60000, n: 1, cpu: 40, mem: 50, disk: 78, net: 10}, {t: Math.floor((now - 2 * 3600_000) / 60000) * 60000, n: 1, cpu: 60, mem: 50, disk: 78, net: 10}]))
    const be = makeFake({hosts: [web]})
    render(<Providers be={be}><ServerPanel host={web} visible /></Providers>)
    expect(await screen.findByText(/The charts show the last/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: '6 hours'}))
    expect(screen.getByText(/a gap is a time it was closed/)).toBeInTheDocument()
    expect(screen.getByRole('button', {name: '6 hours'})).toHaveAttribute('aria-pressed', 'true')
    expect(JSON.parse(localStorage.getItem('vutty.panel.history.h1') || '[]').length).toBeGreaterThanOrEqual(2) // what was kept stays
    localStorage.clear()
  })
})

describe('Server panel: Users', () => {
  it('lists who can log in, with sudo and the keys, and who is logged in now', async () => {
    const be = makeFake({hosts: [web]})
    render(<Providers be={be}><ServerPanel host={web} visible /></Providers>)
    await screen.findByRole('tab', {name: 'Users'})
    await open('Users')
    const root = (await screen.findByText('/root')).closest('tr') as HTMLElement
    expect(within(root).getByText('Yes')).toBeInTheDocument()
    const deploy = screen.getByText('/home/deploy').closest('tr') as HTMLElement
    expect(within(deploy).getByText('?')).toBeInTheDocument() // the file could not be read: not zero keys
    expect(await screen.findByText('203.0.113.7')).toBeInTheDocument()
  })
})
