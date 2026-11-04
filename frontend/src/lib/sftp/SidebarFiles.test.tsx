import {act, fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {TerminalSidebar} from '../terminal/Sidebar'
import {DEFAULT_SETTINGS} from '../settings/defaults'
import {host, makeFake, Providers} from '../../test/fakeBackend'
import {setPaneCwd, setPaneSession} from '../terminal/panes'

type Fake = ReturnType<typeof makeFake>
const web = host({id: 'h1', label: 'web-1', hostname: 'w1.example.com'})
const calls = (be: Fake, n: string) => be.state.calls.filter(c => c[0] === n)

function setup(hostId: string | undefined, init: Parameters<typeof makeFake>[0] = {}, paneId = 'p1') {
  const be = makeFake({hosts: [web], ...init})
  const view = render(
    <Providers be={be}>
      <TerminalSidebar tab="files" hostId={hostId} paneId={paneId} hosts={[web]} snippets={[]} settings={DEFAULT_SETTINGS} appearance="dark" onTab={vi.fn()} onInsert={vi.fn()} />
    </Providers>)
  return {be, ...view}
}
const files = () => screen.getByRole('listbox', {name: 'Files'})
const names = () => within(files()).queryAllByRole('option').map(o => within(o).getByText(/./, {selector: 'b'}).textContent)

describe('terminal sidebar: Files', () => {
  it('is a section of the sidebar', () => {
    setup('h1')
    expect(screen.getByRole('button', {name: 'Files (SFTP)'})).toHaveAttribute('aria-pressed', 'true')
  })

  it('explains itself when the focused terminal is neither local nor a saved SSH host, and opens no connection', () => {
    const {be} = setup('serial:/dev/cu.usbserial-1410')
    expect(screen.getByText(/Focus one to browse its files/)).toBeInTheDocument()
    expect(calls(be, 'sftp.open')).toHaveLength(0)
  })

  it('browses this computer for a local terminal: no SFTP connection, no upload or queue, a file opens with the system', async () => {
    const {be} = setup('local')
    await waitFor(() => expect(names()).toEqual(['Documents', 'notes.txt']))
    expect(calls(be, 'sftp.open')).toHaveLength(0)
    expect(screen.getByLabelText('Current folder')).toHaveTextContent('/Users/me')
    expect(screen.queryByRole('button', {name: 'Upload file…'})).toBeNull()
    await userEvent.dblClick(within(files()).getByRole('option', {name: /Documents/}))
    await waitFor(() => expect(names()).toEqual(['cv.pdf']))
    expect(screen.getByLabelText('Current folder')).toHaveTextContent('/Users/me/Documents')
  })

  it('jumps to the folder a local shell is in, asked of the shell itself', async () => {
    const {be} = setup('local', {}, 'pane-local')
    be.state.shellCwd = '/Users/me/Documents'
    await waitFor(() => expect(names()).toHaveLength(2))
    act(() => setPaneSession('pane-local', 's1'))
    await userEvent.click(screen.getByRole('button', {name: "Go to the terminal's folder"}))
    await waitFor(() => expect(names()).toEqual(['cv.pdf']))
    expect(calls(be, 'session.cwd')[0][1]).toBe('s1')
    act(() => setPaneSession('pane-local', null))
  })

  it('on a server it prefers the folder the shell reported (OSC 7), else asks the server, else says so', async () => {
    const {be} = setup('h1', {}, 'pane-cwd')
    await waitFor(() => expect(names()).toHaveLength(2))
    const go = screen.getByRole('button', {name: "Go to the terminal's folder"})
    await userEvent.click(go)
    expect(await screen.findByRole('alert')).toHaveTextContent('Could not tell which folder the terminal is in.')
    be.state.shellCwd = '/projects'
    act(() => setPaneSession('pane-cwd', 's2'))
    await userEvent.click(go)
    await waitFor(() => expect(names()).toEqual(['app.log']))
    expect(calls(be, 'session.cwd')[0][1]).toBe('s2')
    await userEvent.click(screen.getByRole('button', {name: 'Up one folder'}))
    await waitFor(() => expect(names()).toHaveLength(2))
    act(() => setPaneCwd('pane-cwd', '/projects'))
    be.state.shellCwd = '/'
    await userEvent.click(go)
    await waitFor(() => expect(names()).toEqual(['app.log']))
    expect(calls(be, 'session.cwd')).toHaveLength(1)
    act(() => { setPaneCwd('pane-cwd', null); setPaneSession('pane-cwd', null) })
  })

  it('connects to the focused terminal\'s host and lists its home folder, folders first, hidden files off', async () => {
    const be = setup('h1').be
    await waitFor(() => expect(names()).toEqual(['projects', 'readme.txt']))
    expect(calls(be, 'sftp.open')[0][1]).toBe('h1')
    expect(screen.getByLabelText('Current folder')).toHaveTextContent('/')
  })

  it('opens a folder with a double click, and Up goes back', async () => {
    setup('h1')
    await waitFor(() => expect(names()).toHaveLength(2))
    await userEvent.dblClick(within(files()).getByRole('option', {name: /projects/}))
    await waitFor(() => expect(names()).toEqual(['app.log']))
    expect(screen.getByLabelText('Current folder')).toHaveTextContent('/projects')
    await userEvent.click(screen.getByRole('button', {name: 'Up one folder'}))
    await waitFor(() => expect(names()).toEqual(['projects', 'readme.txt']))
  })

  it('Upload file… queues the picked file into the open folder and the list shows it when done', async () => {
    const {be} = setup('h1', {local: {'/': {dir: true, size: 0}, '/tmp': {dir: true, size: 0}, '/tmp/picked': {dir: false, size: 77}}})
    await waitFor(() => expect(names()).toHaveLength(2))
    await userEvent.click(screen.getByRole('button', {name: 'Upload file…'}))
    await waitFor(() => expect(calls(be, 'sftp.enqueue')).toHaveLength(1))
    expect(calls(be, 'sftp.enqueue')[0][2]).toEqual({direction: 'upload', sources: ['/tmp/picked'], destDir: '/', overwrite: false})
    await waitFor(() => expect(names()).toContain('picked'))
    expect(within(screen.getByRole('region', {name: 'Transfers'})).getByRole('listitem', {name: 'picked'})).toHaveTextContent('Done')
  })

  it('Upload folder… picks a folder, and asks once before replacing what exists', async () => {
    const {be} = setup('h1', {remote: {'/': {dir: true, size: 0}, '/dir': {dir: true, size: 0}, '/dir/a.txt': {dir: false, size: 1}},
      local: {'/': {dir: true, size: 0}, '/tmp': {dir: true, size: 0}, '/tmp/dir': {dir: true, size: 0}, '/tmp/dir/a.txt': {dir: false, size: 9}}})
    await waitFor(() => expect(names()).toEqual(['dir']))
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Upload folder…'}))
    await waitFor(() => expect(ask).toHaveBeenCalledTimes(1))
    expect(ask.mock.calls[0][0]).toBe('dir/a.txt already exists in /. Replace it?')
    await waitFor(() => expect(calls(be, 'sftp.enqueue')).toHaveLength(1))
    expect(calls(be, 'sftp.enqueue')[0][2]).toMatchObject({direction: 'upload', overwrite: true})
    await waitFor(() => expect(be.state.remote['/dir/a.txt'].size).toBe(9))
  })

  it('Download saves the selected items into a folder the user picks', async () => {
    const be = setup('h1').be
    await waitFor(() => expect(names()).toHaveLength(2))
    expect(screen.getByRole('button', {name: 'Download selected files'})).toBeDisabled()
    await userEvent.click(within(files()).getByRole('option', {name: /readme\.txt/}))
    fireEvent.click(within(files()).getByRole('option', {name: /projects/}), {ctrlKey: true})
    expect(screen.getByRole('button', {name: 'Download selected files'})).toHaveTextContent('Download 2')
    await userEvent.click(screen.getByRole('button', {name: 'Download selected files'}))
    await waitFor(() => expect(calls(be, 'sftp.enqueue')).toHaveLength(1))
    expect(calls(be, 'sftp.enqueue')[0][2]).toEqual({direction: 'download', sources: ['/projects', '/readme.txt'], destDir: '/tmp/dir', overwrite: false})
  })

  it('shows the transfer with its buttons, in the sidebar too', async () => {
    const {be} = setup('h1', {holdQueue: true, local: {'/': {dir: true, size: 0}, '/tmp': {dir: true, size: 0}, '/tmp/picked': {dir: false, size: 77}}})
    await waitFor(() => expect(names()).toHaveLength(2))
    await userEvent.click(screen.getByRole('button', {name: 'Upload file…'}))
    const job = await screen.findByRole('listitem', {name: 'picked'})
    expect(job).toHaveTextContent('Uploading · 40%')
    await userEvent.click(within(job).getByRole('button', {name: 'Pause picked'}))
    await waitFor(() => expect(calls(be, 'sftp.pause')).toHaveLength(1))
    await waitFor(() => expect(screen.getByRole('listitem', {name: 'picked'})).toHaveTextContent('Paused'))
  })

  it('hides files that start with a dot, and the eye shows them (remembered)', async () => {
    setup('local', {local: {'/': {dir: true, size: 0}, '/Users': {dir: true, size: 0}, '/Users/me': {dir: true, size: 0}, '/Users/me/notes.txt': {dir: false, size: 3}, '/Users/me/.zshrc': {dir: false, size: 1}}})
    await waitFor(() => expect(names()).toEqual(['notes.txt']))
    await userEvent.click(screen.getByRole('button', {name: 'Show hidden files'}))
    expect(names()).toEqual(['.zshrc', 'notes.txt'])
    expect(screen.getByRole('button', {name: 'Hide hidden files'})).toHaveAttribute('aria-pressed', 'true')
    localStorage.removeItem('vutty.sidebar.files.hidden')
  })

  it('right-click on a file offers what the SFTP screen offers: download, upload, rename, delete, new folder, hidden files', async () => {
    const {be} = setup('h1')
    await waitFor(() => expect(names()).toHaveLength(2))
    fireEvent.contextMenu(within(files()).getByRole('option', {name: /readme\.txt/}))
    const menu = screen.getByRole('menu', {name: 'File menu'})
    for (const n of ['Open', 'Open with…', 'Edit and save back', 'Download', 'Upload file…', 'Upload folder…', 'Copy path', 'Rename', 'Delete', 'Refresh', 'New Folder', 'Show Hidden Files', 'Edit Permissions', 'Select All'])
      expect(within(menu).getByRole('menuitem', {name: n})).toBeInTheDocument()
    await userEvent.click(within(menu).getByRole('menuitem', {name: 'Download'}))
    await waitFor(() => expect(calls(be, 'sftp.enqueue')).toHaveLength(1))
    expect(calls(be, 'sftp.enqueue')[0][2]).toMatchObject({direction: 'download', sources: ['/readme.txt']})
    expect(screen.queryByRole('menu')).toBeNull()
  })

  it('right-click: Rename and New Folder ask for a name, Delete asks first', async () => {
    const {be} = setup('h1')
    await waitFor(() => expect(names()).toHaveLength(2))
    fireEvent.contextMenu(within(files()).getByRole('option', {name: /readme\.txt/}))
    await userEvent.click(screen.getByRole('menuitem', {name: 'Rename'}))
    const input = screen.getByLabelText('Name')
    await userEvent.clear(input)
    await userEvent.type(input, 'notes.txt{Enter}')
    await waitFor(() => expect(calls(be, 'sftp.rename')[0].slice(2)).toEqual(['/readme.txt', '/notes.txt']))
    fireEvent.contextMenu(files())
    expect(screen.getByRole('menu', {name: 'Folder menu'})).toBeInTheDocument()
    await userEvent.click(screen.getByRole('menuitem', {name: 'New Folder'}))
    await userEvent.type(screen.getByLabelText('Name'), 'logs{Enter}')
    await waitFor(() => expect(calls(be, 'sftp.mkdir')[0].slice(2)).toEqual(['/logs']))
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(true)
    fireEvent.contextMenu(within(files()).getByRole('option', {name: /notes\.txt/}))
    await userEvent.click(screen.getByRole('menuitem', {name: 'Delete'}))
    await waitFor(() => expect(calls(be, 'sftp.remove')).toHaveLength(1))
    expect(ask).toHaveBeenCalled()
  })

  it('closes its connection when the section goes away, and offers Reconnect when the connection is lost', async () => {
    const {be, unmount} = setup('h1')
    await waitFor(() => expect(names()).toHaveLength(2))
    const sid = calls(be, 'sftp.list')[0][1] as string
    act(() => be.emit('sftp:closed:' + sid))
    expect(await screen.findByRole('alert')).toHaveTextContent('The connection was lost.')
    await userEvent.click(screen.getByRole('button', {name: 'Reconnect'}))
    await waitFor(() => expect(calls(be, 'sftp.open')).toHaveLength(2))
    await waitFor(() => expect(names()).toHaveLength(2))
    unmount()
    expect(calls(be, 'sftp.close').length).toBeGreaterThan(0)
  })
})
