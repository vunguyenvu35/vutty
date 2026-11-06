import {render, screen, waitFor} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {HostForm} from './HostForm'
import {ProtocolScreen} from '../terminal/ProtocolScreen'
import {host, makeFake, Providers} from '../../test/fakeBackend'

type Fake = ReturnType<typeof makeFake>
const calls = (be: Fake, n: string) => be.state.calls.filter(c => c[0] === n)
const saved = (be: Fake) => calls(be, 'hosts.saveHost')[0][1] as Record<string, unknown>

function form(h?: ReturnType<typeof host>) {
  const be = makeFake({})
  render(<Providers be={be}><HostForm host={h} groups={[]} hosts={h ? [h] : []} identities={[]} onSaved={vi.fn()} onClose={vi.fn()} onConnect={vi.fn()} onDraft={vi.fn()} /></Providers>)
  return be
}

describe('HostForm: VNC', () => {
  it('is off until "Add VNC" is pressed; then 5900, through the SSH login, with no password field', async () => {
    const be = form()
    expect(screen.queryByLabelText('VNC port')).toBeNull()
    await userEvent.type(screen.getByLabelText('Hostname'), 'desk.example.com')
    await userEvent.click(screen.getByRole('button', {name: 'Add VNC'}))
    expect(screen.getByLabelText('VNC port')).toHaveValue('5900')
    expect(screen.getByRole('checkbox', {name: /Through the SSH login/})).toBeChecked()
    expect(screen.getByRole('note')).toHaveTextContent(/inside the encrypted SSH connection/)
    expect(screen.queryByLabelText(/VNC password/)).toBeNull()
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    await waitFor(() => expect(saved(be)).toMatchObject({vnc_enabled: true, vnc_port: 5900, vnc_via_ssh: true}))
  })
  it('direct VNC warns that it is not encrypted and is saved without the tunnel', async () => {
    const be = form()
    await userEvent.type(screen.getByLabelText('Hostname'), 'desk.example.com')
    await userEvent.click(screen.getByRole('button', {name: 'Add VNC'}))
    await userEvent.click(screen.getByRole('checkbox', {name: /Through the SSH login/}))
    expect(screen.getByRole('note')).toHaveTextContent(/Direct VNC is not encrypted/)
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    await waitFor(() => expect(saved(be)).toMatchObject({vnc_enabled: true, vnc_via_ssh: false}))
  })
  it('refuses a port that is not a port; editing shows what was saved and Remove turns it off', async () => {
    const be = form(host({id: 'h1', label: 'd', hostname: 'd', vnc_enabled: true, vnc_port: 5901, vnc_via_ssh: false} as never))
    expect(screen.getByLabelText('VNC port')).toHaveValue('5901')
    expect(screen.getByRole('checkbox', {name: /Through the SSH login/})).not.toBeChecked()
    await userEvent.clear(screen.getByLabelText('VNC port'))
    await userEvent.type(screen.getByLabelText('VNC port'), '0')
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    expect(await screen.findByRole('alert')).toHaveTextContent('VNC port must be between 1 and 65535')
    expect(calls(be, 'hosts.saveHost')).toHaveLength(0)
    await userEvent.click(screen.getByRole('button', {name: 'Remove'}))
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    await waitFor(() => expect(saved(be)).toMatchObject({vnc_enabled: false, vnc_port: 0, vnc_via_ssh: false}))
  })
})

describe('the protocol screen: VNC', () => {
  const choose = (over: object) => {
    const onContinue = vi.fn()
    render(<Providers be={makeFake({})}><ProtocolScreen host={host({id: 'h1', label: 'd', hostname: 'd', ...over} as never)} onContinue={onContinue} onClose={vi.fn()} /></Providers>)
    return onContinue
  }
  it('offers VNC, says it is a screen, and warns when it is direct', async () => {
    const onContinue = choose({vnc_enabled: true, vnc_port: 5900})
    await userEvent.click(screen.getByRole('radio', {name: /VNC/}))
    expect(screen.getByRole('note')).toHaveTextContent(/VNC connection is not encrypted/)
    await userEvent.click(screen.getByRole('button', {name: 'Continue'}))
    expect(onContinue).toHaveBeenCalledWith('vnc')
  })
  it('no warning through SSH, and not available without VNC', async () => {
    choose({vnc_enabled: true, vnc_via_ssh: true})
    await userEvent.click(screen.getByRole('radio', {name: /VNC/}))
    expect(screen.queryByRole('note')).toBeNull()
  })
  it('is not offered on a host without it', () => {
    choose({})
    expect(screen.queryByRole('radio', {name: /VNC/})).toBeNull()
  })
})
