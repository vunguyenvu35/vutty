import {screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {calls, openOn} from '../../test/k8sKit'

const openPod = async (o: Parameters<typeof openOn>[1] = {}) => {
  const r = await openOn('Pods', o)
  await userEvent.click(await screen.findByRole('button', {name: 'web-6b7d9-m4zwn'}))
  await screen.findByRole('heading', {name: 'web-6b7d9-m4zwn'})
  return r
}

describe('Debug container', () => {
  it('asks first, then adds the container and types the kubectl debug line into a tab of the host', async () => {
    const {be, onShell} = await openPod()
    await userEvent.click(screen.getByRole('button', {name: 'Debug'}))
    const dlg = await screen.findByRole('dialog', {name: 'Debug web-6b7d9-m4zwn'})
    expect(calls(be, 'server.k8sDebugStart')).toHaveLength(0) // nothing before the person says so
    expect(within(dlg).getByText(/cannot be removed: it goes away when the pod does/)).toBeInTheDocument()
    const image = within(dlg).getByRole('combobox', {name: 'Image of the tool box'})
    expect(image).toHaveValue('nicolaka/netshoot')
    await userEvent.clear(image)
    await userEvent.type(image, 'busybox')
    await userEvent.selectOptions(within(dlg).getByRole('combobox', {name: 'See the processes of'}), 'web')
    await userEvent.click(within(dlg).getByRole('button', {name: 'Add and open a shell'}))
    await waitFor(() => expect(onShell).toHaveBeenCalledTimes(1))
    expect(calls(be, 'server.k8sDebugStart')).toEqual([['server.k8sDebugStart', 'prod-cluster', 'shop', 'web-6b7d9-m4zwn', 'busybox', 'web']])
    expect(onShell).toHaveBeenCalledWith(expect.objectContaining({id: 'h1'}), be.state.k8s.debug.command, 'web-1 · web-6b7d9-m4zwn · debug')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('cancels without asking the cluster, and says what went wrong', async () => {
    const {be, onShell} = await openPod()
    await userEvent.click(screen.getByRole('button', {name: 'Debug'}))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', {name: 'Cancel'}))
    expect(calls(be, 'server.k8sDebugStart')).toHaveLength(0)
    be.state.k8s.fail = 'the debug container cannot start: ImagePullBackOff'
    await userEvent.click(screen.getByRole('button', {name: 'Debug'}))
    await userEvent.click(within(await screen.findByRole('dialog')).getByRole('button', {name: 'Add and open a shell'}))
    expect(await screen.findByText(/ImagePullBackOff/)).toBeInTheDocument()
    expect(onShell).not.toHaveBeenCalled()
  })

  it('is locked, with the reason, for a login that may not patch pods/ephemeralcontainers', async () => {
    await openPod({rights: {debug: false}})
    const b = screen.getByRole('button', {name: 'Debug'})
    expect(b).toBeDisabled()
    expect(b).toHaveAttribute('title', 'This login may not add a debug container to pods')
  })
})
