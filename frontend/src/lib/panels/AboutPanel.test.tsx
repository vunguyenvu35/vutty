import {act, render, screen, waitFor} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {AboutPanel} from './AboutPanel'
import {makeFake, Providers} from '../../test/fakeBackend'

function setup(extra: Parameters<typeof makeFake>[0] = {}) {
  const be = makeFake({latest: '9.9.9', ...extra})
  render(<Providers be={be}><AboutPanel onClose={vi.fn()} checkNow /></Providers>)
  return be
}
const calls = (be: ReturnType<typeof makeFake>, n: string) => be.state.calls.filter(c => c[0] === n)

describe('About: updating by itself', () => {
  it('offers Install and restart when a newer version is out and this copy can replace itself', async () => {
    const be = setup({selfUpdate: {supported: true}})
    expect(await screen.findByText(/is available/)).toBeInTheDocument()
    const btn = await screen.findByRole('button', {name: 'Install and restart'})
    await userEvent.click(btn)
    await waitFor(() => expect(calls(be, 'system.installUpdate')).toHaveLength(1))
    expect(screen.queryByRole('button', {name: 'Install and restart'})).toBeNull() // while it installs
    expect(screen.getByRole('progressbar', {name: 'Update'})).toBeInTheDocument()
  })

  it('shows what the update is doing, from the progress events, and the percentage of the download', async () => {
    const be = setup({selfUpdate: {supported: true}})
    await userEvent.click(await screen.findByRole('button', {name: 'Install and restart'}))
    const bar = await screen.findByRole('progressbar', {name: 'Update'})
    act(() => be.emit('update:progress', {stage: 'downloading', done: 25, total: 100}))
    expect(bar).toHaveTextContent('Downloading… 25%')
    act(() => be.emit('update:progress', {stage: 'downloading', done: 500, total: 100})) // never over 100
    expect(bar).toHaveTextContent('Downloading… 100%')
    act(() => be.emit('update:progress', {stage: 'verifying'}))
    expect(bar).toHaveTextContent('Checking the signature and the checksum…')
    act(() => be.emit('update:progress', {stage: 'installing'}))
    expect(bar).toHaveTextContent('Installing…')
    act(() => be.emit('update:progress', {stage: 'restarting'}))
    expect(bar).toHaveTextContent('Restarting Vutty…')
  })

  it('puts the button back and shows the reason when the update fails (a signature that does not match, say)', async () => {
    const be = setup({selfUpdate: {supported: true}, installError: 'the signature of this release is not valid, so it was not installed'})
    await userEvent.click(await screen.findByRole('button', {name: 'Install and restart'}))
    expect(await screen.findByRole('alert')).toHaveTextContent('signature of this release is not valid')
    expect(screen.queryByRole('progressbar')).toBeNull()
    expect(screen.getByRole('button', {name: 'Install and restart'})).toBeInTheDocument() // can be tried again
    be.state.installError = undefined
    await userEvent.click(screen.getByRole('button', {name: 'Install and restart'}))
    await waitFor(() => expect(calls(be, 'system.installUpdate')).toHaveLength(2))
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('does not offer it when this copy cannot replace itself, and says why; the release page is still there', async () => {
    const be = setup({selfUpdate: {supported: false, reason: 'Vutty is running from a disk image: drag it to your Applications folder and open it from there'}})
    expect(await screen.findByText(/is available/)).toBeInTheDocument()
    expect(await screen.findByText(/It cannot be installed from here: Vutty is running from a disk image/)).toBeInTheDocument()
    expect(screen.queryByRole('button', {name: 'Install and restart'})).toBeNull()
    await userEvent.click(screen.getByRole('button', {name: 'Open the release page'}))
    expect(calls(be, 'system.openUrl').length).toBeGreaterThan(0)
  })

  it('offers nothing when you are on the latest version', async () => {
    setup({latest: '1.1.0-test', selfUpdate: {supported: true}})
    expect(await screen.findByText('You are on the latest version.')).toBeInTheDocument()
    expect(screen.queryByRole('button', {name: 'Install and restart'})).toBeNull()
  })
})
