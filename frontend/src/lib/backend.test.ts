
type Call = [string, string, string]

function install(reply: (c: Call) => string | Promise<string>) {
  const calls: Call[] = []
  const w = window as any
  w.go = {
    app: {
      SettingsBridge: {Call: vi.fn(async (a: string, b: string, c: string) => { calls.push([a, b, c]); return reply([a, b, c]) })},
      SystemService: {Info: async () => ({version: '9.9.9'}), OpenURL: async () => {}},
    },
  }
  return calls
}

afterEach(() => { delete (window as any).go })

describe('toggleMaximise', () => {
  afterEach(() => { delete (window as any).runtime })
  it('zooms the native window through the Wails runtime', async () => {
    const toggle = vi.fn()
    ;(window as any).runtime = {EventsOn: () => () => {}, WindowToggleMaximise: toggle}
    const {wailsBackend} = await import('./backend')
    await wailsBackend().system.toggleMaximise()
    expect(toggle).toHaveBeenCalledTimes(1)
  })
  it('does nothing in a browser, where there is no runtime', async () => {
    const {wailsBackend} = await import('./backend')
    await expect(wailsBackend().system.toggleMaximise()).resolves.toBeUndefined()
  })
})

describe('app window (Duplicate in new window)', () => {
  afterEach(() => { delete (window as any).go })
  it('is recognised by its own bound object, and every use case call goes to the main process', async () => {
    const {inTerminalWindow, installWindowBridge, wailsBackend, windowTarget} = await import('./backend')
    expect(inTerminalWindow()).toBe(false)
    const calls: [string, string, string][] = []
    ;(window as any).go = {app: {
      TerminalWindow: {
        Call: async (s: string, m: string, a: string) => { calls.push([s, m, a]); return m === 'Open' ? '"sid-1"' : m === 'ListHosts' ? '[{"id":"h1","label":"web"}]' : m === 'Lock' ? '' : 'null' },
        Target: async () => ({host_id: 'h1', title: 'web'}),
      },
      SystemService: {Info: async () => ({version: '1'})},
    }}
    expect(inTerminalWindow()).toBe(true)
    expect(await windowTarget()).toEqual({host_id: 'h1', title: 'web'})
    installWindowBridge()
    const be = wailsBackend() // the same backend as the main window's: only where the calls go differs
    expect(await be.session.open('h1', 80, 24)).toBe('sid-1')
    await be.session.write('sid-1', 'ls\r')
    expect(await be.hosts.listHosts({})).toEqual([{id: 'h1', label: 'web'}])
    await be.vault.lock() // an empty answer is fine
    await be.sftp.mkdir('s', '/x')
    await be.local.home()
    expect(calls.map(c => c[0] + '.' + c[1])).toEqual(['SessionService.Open', 'SessionService.Write', 'HostService.ListHosts', 'VaultService.Lock', 'SftpService.Mkdir', 'LocalService.Home'])
    expect(JSON.parse(calls[1][2])).toEqual(['sid-1', 'ls\r'])
    expect(await be.system.info()).toEqual({version: '1'}) // native things stay in this process
  })
})
