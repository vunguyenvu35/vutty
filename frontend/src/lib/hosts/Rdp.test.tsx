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

describe('HostForm: Remote Desktop', () => {
  it('is off until "Add Remote Desktop" is pressed; then port 3389 and a user name, and no password field', async () => {
    const be = form()
    expect(screen.queryByLabelText('Remote Desktop port')).toBeNull()
    await userEvent.type(screen.getByLabelText('Hostname'), 'win.example.com')
    await userEvent.click(screen.getByRole('button', {name: 'Add Remote Desktop'}))
    expect(screen.getByLabelText('Remote Desktop port')).toHaveValue('3389')
    expect(screen.getByRole('note')).toHaveTextContent(/does not keep an RDP password/)
    expect(screen.queryByLabelText(/Remote Desktop password/)).toBeNull()
    await userEvent.type(screen.getByLabelText('Remote Desktop username'), 'DOM\\ann')
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    await waitFor(() => expect(saved(be)).toMatchObject({rdp_enabled: true, rdp_port: 3389, rdp_username: 'DOM\\ann'}))
  })
  it('refuses a port that is not a port, and Remove turns it off', async () => {
    const be = form(host({id: 'h1', label: 'w', hostname: 'w', rdp_enabled: true, rdp_port: 3390} as never))
    expect(screen.getByLabelText('Remote Desktop port')).toHaveValue('3390')
    await userEvent.clear(screen.getByLabelText('Remote Desktop port'))
    await userEvent.type(screen.getByLabelText('Remote Desktop port'), '99999')
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    expect(await screen.findByRole('alert')).toHaveTextContent('Remote Desktop port must be between 1 and 65535')
    expect(calls(be, 'hosts.saveHost')).toHaveLength(0)
    await userEvent.click(screen.getByRole('button', {name: 'Remove'}))
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    await waitFor(() => expect(saved(be)).toMatchObject({rdp_enabled: false, rdp_port: 0, rdp_username: ''}))
  })
})

describe('the protocol screen: RDP', () => {
  it('offers RDP when the host has it, and says it opens another app', async () => {
    const onContinue = vi.fn()
    render(<Providers be={makeFake({})}><ProtocolScreen host={host({id: 'h1', label: 'w', hostname: 'w', rdp_enabled: true, rdp_port: 3389} as never)} onContinue={onContinue} onClose={vi.fn()} /></Providers>)
    const rdp = screen.getByRole('radio', {name: /RDP/})
    expect(rdp).toHaveAttribute('aria-disabled', 'false')
    await userEvent.click(rdp)
    await userEvent.click(screen.getByRole('button', {name: 'Continue'}))
    expect(onContinue).toHaveBeenCalledWith('rdp')
  })
  it('is not offered on a host without it', () => {
    render(<Providers be={makeFake({})}><ProtocolScreen host={host({id: 'h1', label: 'w', hostname: 'w'})} onContinue={vi.fn()} onClose={vi.fn()} /></Providers>)
    expect(screen.queryByRole('radio', {name: /RDP/})).toBeNull()
  })
})
