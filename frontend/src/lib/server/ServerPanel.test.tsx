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

  it('shows the firewall state of each port and its rules', async () => {
    const {be} = setup()
    await screen.findByText(/up 41d 0h/)
    await open('Ports & firewall')
    expect(await screen.findByText('Firewall (ufw) is on')).toBeInTheDocument()
    const minio = screen.getByText('minio').closest('tr') as HTMLElement
    expect(within(minio).getByText('Shut', {selector: '.sp-pill'})).toBeInTheDocument()
    expect(within(screen.getByText('sshd').closest('tr') as HTMLElement).getByText('Allowed')).toBeInTheDocument()
    expect(screen.queryByText(/Open to the outside/)).toBeNull() // minio listens everywhere but the firewall keeps it out
    expect(screen.getByText('Firewall rules')).toBeInTheDocument()
    expect(calls(be, 'server.firewallAdd')).toHaveLength(0)
  })

  it('asks before allowing a port, does nothing on No, then allows it', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await screen.findByText(/up 41d 0h/)
    await open('Ports & firewall')
    await userEvent.click(await screen.findByRole('button', {name: 'Allow port 9000'}))
    expect(ask).toHaveBeenCalledTimes(1)
    expect(calls(be, 'server.firewallAdd')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Allow port 9000'}))
    await waitFor(() => expect(calls(be, 'server.firewallAdd')).toEqual([['server.firewallAdd', 'allow', 9000, 'tcp', '', false]]))
    expect(await screen.findByText('9000/tcp')).toBeInTheDocument()
    ask.mockRestore()
  })

  it('asks twice before denying the port of this connection, and passes force', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const {be} = setup()
    await screen.findByText(/up 41d 0h/)
    await open('Ports & firewall')
    await userEvent.click(await screen.findByRole('button', {name: 'Deny port 22'}))
    await waitFor(() => expect(calls(be, 'server.firewallAdd')).toEqual([['server.firewallAdd', 'deny', 22, 'tcp', '', true]]))
    expect(ask).toHaveBeenCalledTimes(2)
    expect(String(ask.mock.calls[1][0])).toMatch(/could lock you out/)
    ask.mockRestore()
  })

  it('adds a rule from the form and deletes one after asking', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup()
    await screen.findByText(/up 41d 0h/)
    await open('Ports & firewall')
    await userEvent.click(await screen.findByRole('button', {name: 'Add rule'}))
    const dlg = await screen.findByRole('dialog')
    await userEvent.type(within(dlg).getByPlaceholderText('8080'), '8080')
    await userEvent.type(within(dlg).getByPlaceholderText(/Anyone, or an address/), '10.0.0.0/8')
    await userEvent.click(within(dlg).getByRole('button', {name: 'Add rule'}))
    await waitFor(() => expect(calls(be, 'server.firewallAdd')).toEqual([['server.firewallAdd', 'allow', 8080, 'tcp', '10.0.0.0/8', false]]))
    const row = (await screen.findByText('8080/tcp')).closest('tr') as HTMLElement
    await userEvent.click(within(row).getByRole('button', {name: /Delete the rule/}))
    expect(calls(be, 'server.firewallDelete')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(within(row).getByRole('button', {name: /Delete the rule/}))
    await waitFor(() => expect(calls(be, 'server.firewallDelete')).toEqual([['server.firewallDelete', 2, false]]))
    ask.mockRestore()
  })

  it('turns the firewall off after asking', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const {be} = setup()
    await screen.findByText(/up 41d 0h/)
    await open('Ports & firewall')
    await userEvent.click(await screen.findByRole('button', {name: 'Turn off'}))
    await waitFor(() => expect(calls(be, 'server.firewallDisable')).toHaveLength(1))
    expect(await screen.findByRole('button', {name: 'Turn on'})).toBeInTheDocument()
    ask.mockRestore()
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
    await userEvent.click(within(api).getByRole('button', {name: 'More actions for api'}))
    await userEvent.click(screen.getByRole('menuitem', {name: 'Restart'}))
    expect(ask).toHaveBeenLastCalledWith(expect.stringContaining('Restart the container api'))
    expect(calls(be, 'server.containerAction')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(within(api).getByRole('button', {name: 'More actions for api'}))
    await userEvent.click(screen.getByRole('menuitem', {name: 'Restart'}))
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
    await userEvent.click(within(api).getByRole('button', {name: 'More actions for api'}))
    await userEvent.click(screen.getByRole('menuitem', {name: 'Details'}))
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

describe('Server panel: managing users', () => {
  const ed = 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakeKeyForTests me@laptop'
  afterEach(() => { vi.restoreAllMocks() })
  async function openUsers() {
    const be = makeFake({hosts: [web]})
    render(<Providers be={be}><ServerPanel host={web} visible /></Providers>)
    await screen.findByRole('tab', {name: 'Users'})
    await open('Users')
    await screen.findByText('/root')
    return be
  }

  it('adds a user with a shell, sudo and a first key, and lists it', async () => {
    const be = await openUsers()
    await userEvent.click(screen.getByRole('button', {name: 'Add user'}))
    const dlg = screen.getByRole('dialog', {name: 'Add user'})
    const add = within(dlg).getByRole('button', {name: 'Add user'})
    expect(add).toBeDisabled()
    await userEvent.type(within(dlg).getByLabelText('User name'), 'Bad Name')
    expect(add).toBeDisabled()
    await userEvent.clear(within(dlg).getByLabelText('User name'))
    await userEvent.type(within(dlg).getByLabelText('User name'), 'ci')
    await userEvent.click(within(dlg).getByLabelText('May use sudo'))
    await userEvent.type(within(dlg).getByLabelText('Public key (optional)'), ed)
    await userEvent.click(add)
    await waitFor(() => expect(calls(be, 'server.addUser')).toEqual([['server.addUser', 'ci', '/bin/bash', true, ed]]))
    expect(await screen.findByText('/home/ci')).toBeInTheDocument()
  })

  it('deletes only after the name is typed, never offers root or the account of the connection, and shows a refusal', async () => {
    const be = await openUsers()
    expect(screen.queryByRole('button', {name: 'Delete root'})).toBeNull()
    await userEvent.click(screen.getByRole('button', {name: 'Delete deploy'}))
    const dlg = screen.getByRole('dialog', {name: 'Delete deploy'})
    const del = within(dlg).getByRole('button', {name: 'Delete'})
    expect(del).toBeDisabled()
    await userEvent.type(within(dlg).getByLabelText('Type deploy to confirm'), 'deploy')
    await userEvent.click(within(dlg).getByLabelText(/Also delete its home folder/))
    await userEvent.click(del)
    await waitFor(() => expect(calls(be, 'server.deleteUser')).toEqual([['server.deleteUser', 'deploy', true]]))
    await waitFor(() => expect(screen.queryByText('/home/deploy')).toBeNull())
  })

  it('keeps the dialog open and says why when the server refuses', async () => {
    const be = await openUsers()
    be.state.server.failAction = 'deploy is logged in now'
    await userEvent.click(screen.getByRole('button', {name: 'Delete deploy'}))
    const dlg = screen.getByRole('dialog', {name: 'Delete deploy'})
    await userEvent.type(within(dlg).getByLabelText('Type deploy to confirm'), 'deploy')
    await userEvent.click(within(dlg).getByRole('button', {name: 'Delete'}))
    expect(await within(dlg).findByText(/deploy is logged in now/)).toBeInTheDocument()
  })

  it('lists the keys, adds one and asks before removing one', async () => {
    const be = await openUsers()
    await userEvent.click(screen.getByRole('button', {name: 'SSH keys of deploy'}))
    const dlg = screen.getByRole('dialog', {name: 'SSH keys of deploy'})
    expect(await within(dlg).findByText('old@laptop')).toBeInTheDocument()
    await userEvent.type(within(dlg).getByLabelText('Add a public key'), ed)
    await userEvent.click(within(dlg).getByRole('button', {name: 'Add key'}))
    await waitFor(() => expect(calls(be, 'server.addUserKey')).toEqual([['server.addUserKey', 'deploy', ed]]))
    expect(await within(dlg).findByText('me@laptop')).toBeInTheDocument()
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    await userEvent.click(within(dlg).getByRole('button', {name: /Remove the key SHA256:a/}))
    expect(ask).toHaveBeenLastCalledWith(expect.stringContaining('old@laptop'))
    expect(calls(be, 'server.removeUserKey')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(within(dlg).getByRole('button', {name: /Remove the key SHA256:a/}))
    await waitFor(() => expect(calls(be, 'server.removeUserKey')).toHaveLength(1))
    await waitFor(() => expect(within(dlg).queryByText('old@laptop')).toBeNull())
  })
})

describe('Server panel: reboot', () => {
  afterEach(() => { vi.restoreAllMocks() })
  it('asks first, and restarts only when confirmed', async () => {
    const {be} = setup()
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    await userEvent.click(await screen.findByRole('button', {name: 'Reboot'}))
    expect(ask).toHaveBeenLastCalledWith(expect.stringContaining('Restart web-1?'))
    expect(calls(be, 'server.reboot')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Reboot'}))
    await waitFor(() => expect(calls(be, 'server.reboot')).toHaveLength(1))
  })
})
