import {act, render, screen, waitFor} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {host, makeFake, Providers} from '../../test/fakeBackend'
import {VncViewer} from './VncViewer'

// noVNC draws on a canvas, which jsdom does not have: a stand-in that records what the viewer asks of it
const {made, FakeRFB} = vi.hoisted(() => {
  class FakeRFB extends EventTarget {
    viewOnly = false
    scaleViewport = false
    resizeSession = true
    background = ''
    disconnected = false
    sent: Array<{username?: string; password?: string}> = []
    ctrlAltDel = 0
    constructor(public target: HTMLElement, public url: string, public options?: unknown) { super(); made.push(this) }
    disconnect() { this.disconnected = true }
    sendCredentials(c: {username?: string; password?: string}) { this.sent.push(c) }
    sendCtrlAltDel() { this.ctrlAltDel++ }
    focus() {}
    emit(name: string, detail?: unknown) { this.dispatchEvent(new CustomEvent(name, {detail})) }
  }
  const made: FakeRFB[] = []
  return {made, FakeRFB}
})
vi.mock('@novnc/novnc', () => ({default: FakeRFB}))

type Fake = ReturnType<typeof makeFake>
const calls = (be: Fake, n: string) => be.state.calls.filter(c => c[0] === n)
const vncHost = (over = {}) => host({id: 'h1', label: 'desk', hostname: 'd.example.com', vnc_enabled: true, vnc_port: 5900, vnc_via_ssh: true, ...over} as never)

function view(h = vncHost()) {
  const be = makeFake({hosts: [h]})
  const r = render(<Providers be={be}><VncViewer host={h} visible /></Providers>)
  return {be, ...r}
}
const rfb = () => made[made.length - 1]!
beforeEach(() => { made.length = 0 })

describe('the VNC viewer', () => {
  it('opens a path for the host, connects the viewer to the address it gives, and shows the toolbar once connected', async () => {
    const {be} = view()
    await waitFor(() => expect(made).toHaveLength(1))
    expect(calls(be, 'vnc.open')[0][1]).toBe('h1')
    expect(rfb().url).toBe('ws://127.0.0.1:1/secret')
    expect(rfb().scaleViewport).toBe(true)
    expect(rfb().resizeSession).toBe(false)
    expect(screen.queryByRole('button', {name: 'Ctrl+Alt+Del'})).toBeNull() // nothing to send to yet
    act(() => rfb().emit('connect'))
    expect(await screen.findByRole('button', {name: 'Ctrl+Alt+Del'})).toBeInTheDocument()
    expect(screen.getByText('through SSH')).toBeInTheDocument()
  })

  it('asks for the VNC password in the tab, sends it to the viewer only, and clears it', async () => {
    view()
    await waitFor(() => expect(made).toHaveLength(1))
    act(() => rfb().emit('credentialsrequired', {types: ['password']}))
    const pw = await screen.findByLabelText('VNC password')
    expect(pw).toHaveAttribute('type', 'password')
    expect(screen.queryByLabelText('VNC username')).toBeNull()
    await userEvent.type(pw, 's3cret')
    await userEvent.click(screen.getByRole('button', {name: 'Connect'}))
    expect(rfb().sent).toEqual([{username: undefined, password: 's3cret'}])
    expect(screen.queryByLabelText('VNC password')).toBeNull()
    act(() => rfb().emit('connect'))
    expect(screen.queryByDisplayValue('s3cret')).toBeNull()
  })

  it('asks for a user name too when the server wants one', async () => {
    view()
    await waitFor(() => expect(made).toHaveLength(1))
    act(() => rfb().emit('credentialsrequired', {types: ['username', 'password']}))
    await userEvent.type(await screen.findByLabelText('VNC username'), 'ann')
    await userEvent.type(screen.getByLabelText('VNC password'), 'pw')
    await userEvent.click(screen.getByRole('button', {name: 'Connect'}))
    expect(rfb().sent).toEqual([{username: 'ann', password: 'pw'}])
  })

  it('Fit, View only and Ctrl+Alt+Del act on the viewer', async () => {
    view()
    await waitFor(() => expect(made).toHaveLength(1))
    act(() => rfb().emit('connect'))
    const fit = await screen.findByRole('button', {name: 'Fit'})
    expect(fit).toHaveAttribute('aria-pressed', 'true')
    await userEvent.click(fit)
    expect(rfb().scaleViewport).toBe(false)
    await userEvent.click(screen.getByRole('button', {name: 'View only'}))
    expect(rfb().viewOnly).toBe(true)
    await userEvent.click(screen.getByRole('button', {name: 'Ctrl+Alt+Del'}))
    expect(rfb().ctrlAltDel).toBe(1)
  })

  it('says why it ended, and Reconnect starts a viewer again', async () => {
    const {be} = view()
    await waitFor(() => expect(made).toHaveLength(1))
    act(() => rfb().emit('connect'))
    act(() => rfb().emit('disconnect', {clean: false}))
    expect(await screen.findByRole('alert')).toHaveTextContent('The VNC connection was lost.')
    await userEvent.click(screen.getByRole('button', {name: 'Reconnect'}))
    await waitFor(() => expect(made).toHaveLength(2))
    expect(made[0].disconnected).toBe(true)
    expect(calls(be, 'vnc.open')).toHaveLength(1) // the path is the same: only the viewer starts again
  })

  it('a path that ends (the SSH login was lost) is shown with its reason', async () => {
    const {be} = view()
    await waitFor(() => expect(made).toHaveLength(1))
    act(() => be.emit('vnc:closed:vnc1', {reason: 'the SSH connection was lost'}))
    expect(await screen.findByRole('alert')).toHaveTextContent('the SSH connection was lost')
  })

  it('shows a refusal to open, and closes the path and the viewer when the tab goes away', async () => {
    const be = makeFake({hosts: [vncHost()]})
    be.state.server.failAction = 'VNC is not enabled'
    const first = render(<Providers be={be}><VncViewer host={vncHost()} visible /></Providers>)
    expect(await screen.findByRole('alert')).toHaveTextContent('VNC is not enabled')
    expect(made).toHaveLength(0)
    first.unmount()
    be.state.server.failAction = ''
    const {be: be2, unmount} = view()
    await waitFor(() => expect(made).toHaveLength(1))
    unmount()
    expect(made[0].disconnected).toBe(true)
    await waitFor(() => expect(calls(be2, 'vnc.close')[0][1]).toBe('vnc1'))
  })

  it('says when the connection is not encrypted', async () => {
    view(vncHost({vnc_via_ssh: false}))
    await waitFor(() => expect(made).toHaveLength(1))
    act(() => rfb().emit('connect'))
    expect(await screen.findByText('direct, not encrypted')).toBeInTheDocument()
  })
})
