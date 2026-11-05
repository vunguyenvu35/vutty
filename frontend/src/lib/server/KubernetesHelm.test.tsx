import {screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {calls, openOn} from '../../test/k8sKit'

beforeEach(() => { vi.spyOn(window, 'confirm').mockReturnValue(true) })
const closeDialog = async (name: string) => { await userEvent.click(within(screen.getByRole('dialog', {name})).getAllByRole('button', {name: 'Close'}).at(-1)!) } // the X in the head and the button in the foot

describe('Apps: Helm releases, read only', () => {
  it('lists the releases and opens one with its chart, manifest, notes and history', async () => {
    const {be} = await openOn('Apps')
    const table = await screen.findByRole('table')
    expect(within(table).getByText('deployed')).toHaveClass('ok')
    expect(within(table).getByText('failed')).toHaveClass('bad')
    expect(screen.getByText('2 releases · read only')).toBeInTheDocument()
    expect(screen.getByText(/Chart repositories are kept by Helm or Rancher/)).toBeInTheDocument()
    await userEvent.type(screen.getByRole('textbox', {name: 'Filter releases'}), 'fail')
    expect(within(table).queryByText('web')).not.toBeInTheDocument()
    await userEvent.clear(screen.getByRole('textbox', {name: 'Filter releases'}))

    await userEvent.click(within(table).getByRole('button', {name: 'web'}))
    expect(await screen.findByText('nginx 15.1.0')).toBeInTheDocument()
    expect(calls(be, 'server.k8sHelmRelease').pop()!.slice(1)).toEqual(['prod-cluster', 'shop', 'web', 0])
    await userEvent.click(screen.getByRole('button', {name: 'Manifest'}))
    expect(await screen.findByRole('dialog', {name: 'Manifest of web'})).toHaveTextContent('kind: Deployment')
    await closeDialog('Manifest of web')
    await userEvent.click(screen.getByRole('button', {name: 'Notes'}))
    expect(await screen.findByRole('dialog', {name: 'Notes of web'})).toHaveTextContent('Visit http://web')
    await closeDialog('Notes of web')

    // an older revision can be looked at; the one running has no rollback command, an older one does
    expect(screen.queryByRole('button', {name: /Copy the rollback command to revision 3/})).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Copy the rollback command to revision 2'}))
    await waitFor(() => expect(calls(be, 'system.writeClipboard').pop()).toEqual(['system.writeClipboard', 'helm rollback web 2 -n shop']))
    await userEvent.click(screen.getByRole('button', {name: 'View revision 2'}))
    await waitFor(() => expect(calls(be, 'server.k8sHelmRelease').pop()!.slice(4)).toEqual([2]))
  })

  it('asks before it shows the values, and never asks for them when there are none', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = await openOn('Apps')
    await userEvent.click(await screen.findByRole('button', {name: 'web'}))
    await screen.findByText('nginx 15.1.0')
    await userEvent.click(screen.getByRole('button', {name: 'Values'}))
    expect(ask).toHaveBeenCalledWith(expect.stringContaining('can hold passwords'))
    expect(calls(be, 'server.k8sHelmValues')).toHaveLength(0) // nothing is read until it is confirmed
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Values'}))
    expect(await screen.findByRole('dialog', {name: 'Values of web'})).toHaveTextContent('replicas: 3')
    expect(calls(be, 'server.k8sHelmValues')).toHaveLength(1)
    await closeDialog('Values of web')
    await userEvent.click(screen.getByRole('button', {name: 'Back'}))
    await userEvent.click(await screen.findByRole('button', {name: 'db'}))
    await screen.findByText('postgresql 13.0.0')
    expect(screen.getByRole('button', {name: 'Values'})).toBeDisabled()
    expect(screen.getByRole('button', {name: 'Notes'})).toBeDisabled()
  })
})
