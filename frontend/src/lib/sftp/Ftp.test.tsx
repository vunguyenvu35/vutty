import {render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {HostForm} from '../hosts/HostForm'
import {ProtocolScreen} from '../terminal/ProtocolScreen'
import {SftpBrowser} from './SftpBrowser'
import {host, makeFake, Providers} from '../../test/fakeBackend'

type Fake = ReturnType<typeof makeFake>
const calls = (be: Fake, n: string) => be.state.calls.filter(c => c[0] === n)
const saved = (be: Fake) => calls(be, 'hosts.saveHost')[0][1] as Record<string, unknown>

function form(h?: ReturnType<typeof host>) {
  const be = makeFake({})
  render(<Providers be={be}><HostForm host={h} groups={[]} hosts={h ? [h] : []} identities={[]} onSaved={vi.fn()} onClose={vi.fn()} onConnect={vi.fn()} onDraft={vi.fn()} /></Providers>)
  return be
}

describe('HostForm: FTP', () => {
  it('is off until "Add FTP" is pressed; then it is FTPS with explicit TLS on port 21, and the password goes to the vault, not the host', async () => {
    const be = form()
    expect(screen.queryByLabelText('FTP port')).toBeNull()
    await userEvent.type(screen.getByLabelText('Hostname'), 'files.example.com')
    await userEvent.click(screen.getByRole('button', {name: 'Add FTP'}))
    expect(screen.getByLabelText('FTP port')).toHaveValue('21')
    expect(screen.getByRole('combobox', {name: /FTP security/})).toHaveTextContent('FTPS (explicit TLS)')
    expect(screen.queryByRole('note')).toBeNull() // the warning is for plain FTP only
    await userEvent.type(screen.getByLabelText('FTP username'), 'web')
    const pw = screen.getByLabelText('FTP password')
    await userEvent.type(pw, 's3cret')
    expect(pw).toHaveAttribute('type', 'password')
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    await waitFor(() => expect(saved(be)).toMatchObject({ftp_enabled: true, ftp_port: 21, ftp_security: 'explicit', ftp_username: 'web'}))
    await waitFor(() => expect(calls(be, 'hosts.setFtpPassword')[0].slice(2)).toEqual(['s3cret']))
    expect(JSON.stringify(saved(be))).not.toContain('s3cret')
  })

  it('implicit TLS moves the port to 990, plain FTP warns and is saved with no security mode', async () => {
    const be = form()
    await userEvent.type(screen.getByLabelText('Hostname'), 'files.example.com')
    await userEvent.click(screen.getByRole('button', {name: 'Add FTP'}))
    await userEvent.click(screen.getByRole('combobox', {name: /FTP security/}))
    await userEvent.click(screen.getByRole('option', {name: /implicit TLS/}))
    expect(screen.getByLabelText('FTP port')).toHaveValue('990')
    await userEvent.click(screen.getByRole('combobox', {name: /FTP security/}))
    await userEvent.click(screen.getByRole('option', {name: /Plain FTP/}))
    expect(screen.getByLabelText('FTP port')).toHaveValue('21')
    expect(screen.getByRole('note')).toHaveTextContent(/not encrypted/)
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    await waitFor(() => expect(saved(be)).toMatchObject({ftp_enabled: true, ftp_port: 21, ftp_security: ''}))
  })

  it('refuses a port that is not a port, and an edited plain FTP host stays plain', async () => {
    const be = form()
    await userEvent.type(screen.getByLabelText('Hostname'), 'f.example.com')
    await userEvent.click(screen.getByRole('button', {name: 'Add FTP'}))
    await userEvent.clear(screen.getByLabelText('FTP port'))
    await userEvent.type(screen.getByLabelText('FTP port'), '70000')
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    expect(await screen.findByRole('alert')).toHaveTextContent('FTP port must be between 1 and 65535')
    expect(calls(be, 'hosts.saveHost')).toHaveLength(0)
  })

  it('editing shows what was saved, and Remove turns FTP off', async () => {
    const be = form(host({id: 'h1', label: 'f', hostname: 'f.example.com', ftp_enabled: true, ftp_port: 2121, ftp_username: 'u'} as never))
    expect(screen.getByLabelText('FTP port')).toHaveValue('2121')
    expect(screen.getByRole('combobox', {name: /FTP security/})).toHaveTextContent('Plain FTP') // a host with no mode is plain
    expect(screen.getByRole('note')).toHaveTextContent(/not encrypted/)
    await userEvent.click(screen.getByRole('button', {name: 'Remove'}))
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    await waitFor(() => expect(saved(be)).toMatchObject({ftp_enabled: false, ftp_port: 0, ftp_security: '', ftp_username: ''}))
  })
})

describe('the protocol screen', () => {
  const choose = (h: Parameters<typeof host>[0]) => {
    const onContinue = vi.fn()
    render(<Providers be={makeFake({})}><ProtocolScreen host={host(h)} onContinue={onContinue} onClose={vi.fn()} /></Providers>)
    return onContinue
  }
  it('offers FTP when the host has it, says it is files only, and warns for plain FTP', async () => {
    const onContinue = choose({id: 'h1', label: 'f', hostname: 'f.example.com', ftp_enabled: true, ftp_port: 21} as never)
    const ftp = screen.getByRole('radio', {name: /FTP/})
    expect(ftp).toHaveAttribute('aria-disabled', 'false')
    expect(within(ftp).getByText('Files only')).toBeInTheDocument()
    await userEvent.click(ftp)
    expect(screen.getByRole('note')).toHaveTextContent(/Plain FTP is not encrypted/)
    await userEvent.click(screen.getByRole('button', {name: 'Continue'}))
    expect(onContinue).toHaveBeenCalledWith('ftp')
  })
  it('names it FTPS when the host uses TLS, and cannot be picked when the host has no FTP', async () => {
    choose({id: 'h1', label: 'f', hostname: 'f.example.com', ftp_enabled: true, ftp_port: 21, ftp_security: 'explicit'} as never)
    await userEvent.click(screen.getByRole('radio', {name: /FTPS/}))
    expect(screen.queryByRole('note')).toBeNull()
  })
  it('shows FTP as unavailable on a host without it', () => {
    choose({id: 'h1', label: 'f', hostname: 'f.example.com'})
    expect(screen.getByRole('radio', {name: /FTP/})).toHaveAttribute('aria-disabled', 'true')
  })
})

describe('the SFTP tab and FTP', () => {
  it('lists a host that has FTP twice: over SSH, and "(FTP)", which opens the host with the ftp: id', async () => {
    const be = makeFake({hosts: [host({id: 'h1', label: 'web-1', hostname: 'w1.example.com'}), host({id: 'h2', label: 'files', hostname: 'f.example.com', ftp_enabled: true, ftp_port: 21, ftp_security: 'explicit'} as never)]})
    render(<Providers be={be}><SftpBrowser /></Providers>)
    const R = () => screen.getByRole('region', {name: 'Remote'})
    await userEvent.click(within(R()).getByRole('button', {name: 'Select host'}))
    expect(within(R()).getByRole('button', {name: 'Connect to files'})).toBeInTheDocument()
    expect(within(R()).queryByRole('button', {name: 'Connect to web-1 (FTP)'})).toBeNull() // only a host that has FTP
    expect(within(R()).getByText(/FTPS · f\.example\.com:21/)).toBeInTheDocument()
    await userEvent.click(within(R()).getByRole('button', {name: 'Connect to files (FTP)'}))
    await waitFor(() => expect(calls(be, 'sftp.open')[0][1]).toBe('ftp:h2'))
    expect(await screen.findByTestId('sftp')).toHaveAttribute('aria-label', 'SFTP files (FTP)')
  })
})
