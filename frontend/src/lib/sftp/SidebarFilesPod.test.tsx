import {fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {TerminalSidebar} from '../terminal/Sidebar'
import {DEFAULT_SETTINGS} from '../settings/defaults'
import {host, makeFake, Providers} from '../../test/fakeBackend'

type Fake = ReturnType<typeof makeFake>
const calls = (be: Fake, n: string) => be.state.calls.filter(c => c[0] === n)

function setup() {
  const be = makeFake({hosts: [host({id: 'h1', label: 'web-1', hostname: 'w1.example.com'})]})
  render(
    <Providers be={be}>
      <TerminalSidebar tab="files" hostId="kubeexec:t1" paneId="p1" hosts={[]} snippets={[]} settings={DEFAULT_SETTINGS} appearance="dark" onTab={vi.fn()} onInsert={vi.fn()} />
    </Providers>)
  return be
}
const files = () => screen.getByRole('listbox', {name: 'Files'})
const names = () => within(files()).queryAllByRole('option').map(o => within(o).getByText(/./, {selector: 'b'}).textContent)

describe('terminal sidebar: Files of a shell in a pod', () => {
  it('lists the folder the pod starts in, through the pod and not through SFTP, and goes into a folder', async () => {
    const be = setup()
    await waitFor(() => expect(names()).toEqual(['storage', 'artisan'])) // .env is hidden until asked for
    expect(calls(be, 'server.podFilesHome')[0].slice(1)).toEqual(['kubeexec:t1'])
    expect(calls(be, 'server.podFilesList')[0].slice(1)).toEqual(['kubeexec:t1', '/app'])
    expect(calls(be, 'sftp.open')).toHaveLength(0)
    expect(screen.getByLabelText('Current folder')).toHaveTextContent('/app')
    await userEvent.dblClick(within(files()).getByRole('option', {name: /storage/}))
    await waitFor(() => expect(names()).toEqual(['laravel.log']))
    await userEvent.click(screen.getByRole('button', {name: 'Up one folder'}))
    await waitFor(() => expect(names()).toEqual(['storage', 'artisan']))
  })

  it('can only look and copy out: no upload, no queue, and the menu has nothing that changes a file', async () => {
    setup()
    await waitFor(() => expect(names()).toHaveLength(2))
    expect(screen.queryByRole('button', {name: 'Upload file…'})).toBeNull()
    expect(screen.queryByRole('button', {name: 'Upload folder…'})).toBeNull()
    fireEvent.contextMenu(within(files()).getByRole('option', {name: /artisan/}))
    const menu = await screen.findByRole('menu')
    for (const gone of ['Rename', 'Delete', 'New Folder', 'Edit Permissions', 'Upload file…', 'Upload folder…', 'Edit and save back']) {
      expect(within(menu).queryByRole('menuitem', {name: gone}), gone).toBeNull()
    }
    for (const kept of ['Open', 'Download', 'Copy path', 'Refresh']) expect(within(menu).getByRole('menuitem', {name: kept})).toBeInTheDocument()
  })

  it('copies a file out into the folder picked, and says that a folder cannot be copied yet', async () => {
    const be = setup()
    await waitFor(() => expect(names()).toHaveLength(2))
    await userEvent.click(within(files()).getByRole('option', {name: /artisan/}))
    await userEvent.click(screen.getByRole('button', {name: 'Download selected files'}))
    await waitFor(() => expect(calls(be, 'server.podFilesFetch')).toHaveLength(1))
    expect(calls(be, 'server.podFilesFetch')[0].slice(1, 3)).toEqual(['kubeexec:t1', '/app/artisan'])
    await userEvent.click(within(files()).getByRole('option', {name: /storage/}))
    await userEvent.click(screen.getByRole('button', {name: 'Download selected files'}))
    expect(await screen.findByText(/A folder cannot be copied out of a pod yet/)).toBeInTheDocument()
    expect(calls(be, 'server.podFilesFetch')).toHaveLength(1)
  })

  it('opens a file by copying it into a private temporary folder and handing it to the system', async () => {
    const be = setup()
    await waitFor(() => expect(names()).toHaveLength(2))
    await userEvent.dblClick(within(files()).getByRole('option', {name: /artisan/}))
    await waitFor(() => expect(calls(be, 'system.openPath')).toHaveLength(1))
    expect(calls(be, 'server.podFilesFetch')[0][2]).toBe('/app/artisan')
  })

  it('says why when the pod cannot be read', async () => {
    const be = makeFake({hosts: []})
    be.server.podFilesHome = async () => { throw new Error('this container has no shell, so its files cannot be listed') }
    render(
      <Providers be={be}>
        <TerminalSidebar tab="files" hostId="kubeexec:t1" paneId="p1" hosts={[]} snippets={[]} settings={DEFAULT_SETTINGS} appearance="dark" onTab={vi.fn()} onInsert={vi.fn()} />
      </Providers>)
    expect(await screen.findByText(/has no shell/)).toBeInTheDocument()
    expect(screen.getByRole('button', {name: 'Reconnect'})).toBeInTheDocument()
  })
})
