import {render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {calls, connect, openKubernetes, subTab} from '../../test/k8sKit'
import {Providers} from '../../test/fakeBackend'
import {ServerPanel} from './ServerPanel'
import {makeFake} from '../../test/fakeBackend'
import {web} from '../../test/k8sKit'

beforeEach(() => { vi.spyOn(window, 'confirm').mockReturnValue(true) })
afterEach(() => vi.restoreAllMocks())

describe('Kubernetes tab: where it is', () => {
  it('is the tab right after Docker', async () => {
    await openKubernetes()
    const names = within(screen.getByRole('navigation', {name: 'Server sections'})).getAllByRole('tab').map(t => t.getAttribute('aria-label'))
    expect(names.indexOf('Kubernetes')).toBe(names.indexOf('Docker') + 1)
    expect(screen.getByRole('tab', {name: 'Kubernetes'})).toHaveAttribute('aria-selected', 'true')
  })
})

describe('Kubernetes: connect flow', () => {
  it('finds kubectl, asks which cluster, remembers it, shows what the login may do, then opens', async () => {
    const be = makeFake({hosts: [web]})
    be.state.k8s.rights = {delete_pod: false, secrets: false}
    await openKubernetes({be})
    expect(await screen.findByRole('radiogroup', {name: 'Cluster'})).toBeInTheDocument()
    expect(screen.getByText('This kubeconfig has 3 contexts. The one you choose is used for everything in this tab.')).toBeInTheDocument()
    expect(screen.getByRole('radio', {name: /kind-dev/})).toHaveAttribute('aria-checked', 'true') // the current one of the kubeconfig
    expect(screen.getByText('production')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('radio', {name: /prod-cluster/}))
    expect(screen.getByRole('checkbox', {name: /Remember this choice for web-1/})).toBeChecked()
    await userEvent.click(screen.getByRole('button', {name: 'Continue'}))
    expect(await screen.findByRole('heading', {name: 'What this login may do on prod-cluster'})).toBeInTheDocument()
    expect(calls(be, 'server.k8sCheck')).toEqual([['server.k8sCheck', 'prod-cluster']])
    expect(within(screen.getByText('delete pods').closest('tr') as HTMLElement).getByText('No')).toBeInTheDocument()
    expect(within(screen.getByText('get pods/log').closest('tr') as HTMLElement).getByText('Yes')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Open Kubernetes'}))
    expect(await screen.findByText('41 / 44')).toBeInTheDocument() // the overview
    expect(localStorage.getItem('vutty.k8s.context.h1')).toBe('prod-cluster')
    expect(screen.getByText('server v1.30.2 · kubectl v1.30.4')).toBeInTheDocument()
  })

  it('goes straight in when the cluster of this host is remembered', async () => {
    const be = await openKubernetes({remember: 'staging-cluster'})
    expect(await screen.findByRole('tablist', {name: 'Kubernetes views'})).toBeInTheDocument()
    expect(screen.queryByRole('radiogroup')).toBeNull()
    expect(calls(be.be, 'server.k8sCheck')).toEqual([['server.k8sCheck', 'staging-cluster']])
  })

  it('does not remember when the box is unticked', async () => {
    await openKubernetes()
    await userEvent.click(await screen.findByRole('radio', {name: /staging/}))
    await userEvent.click(screen.getByRole('checkbox', {name: /Remember/}))
    await userEvent.click(screen.getByRole('button', {name: 'Continue'}))
    await userEvent.click(await screen.findByRole('button', {name: 'Open Kubernetes'}))
    await screen.findByRole('tablist', {name: 'Kubernetes views'})
    expect(localStorage.getItem('vutty.k8s.context.h1')).toBeNull()
  })

  it('skips the choice when the kubeconfig has one context, and still shows the rights', async () => {
    const be = makeFake({hosts: [web]})
    be.state.k8s.contexts.contexts = be.state.k8s.contexts.contexts.slice(0, 1)
    await openKubernetes({be})
    expect(await screen.findByRole('heading', {name: 'What this login may do on prod-cluster'})).toBeInTheDocument()
  })

  it('says so when kubectl is missing, and looks again on Check again', async () => {
    const be = makeFake({hosts: [web]})
    be.state.k8s.detect = {found: false, cmd: '', path: '', client_version: '', tried: ['kubectl', 'k3s kubectl']}
    await openKubernetes({be})
    expect(await screen.findByRole('heading', {name: 'kubectl was not found on this server'})).toBeInTheDocument()
    expect(screen.getByText('Tried: kubectl, k3s kubectl')).toBeInTheDocument()
    expect(calls(be, 'server.k8sContexts')).toHaveLength(0)
    be.state.k8s.detect = {...be.state.k8s.detect, found: true, cmd: 'kubectl', path: '/usr/bin/kubectl', client_version: 'v1.30.4'}
    await userEvent.click(screen.getByRole('button', {name: 'Check again'}))
    expect(await screen.findByRole('radiogroup', {name: 'Cluster'})).toBeInTheDocument()
  })

  it.each([
    ['unreachable', 'The API server does not answer', 'Unable to connect to the server: dial tcp 10.0.0.11:6443: i/o timeout'],
    ['unauthorized', 'The login is not accepted', 'error: You must be logged in to the server (Unauthorized)'],
    ['certificate', 'The certificate does not match', 'x509: certificate is valid for 10.96.0.1, not 10.0.0.11'],
    ['forbidden', 'This login may not use the cluster', 'nodes is forbidden'],
    ['other', 'The cluster could not be asked', 'something odd'],
  ] as const)('shows the %s card with the raw message and checks again', async (kind, title, raw) => {
    const be = makeFake({hosts: [web]})
    be.state.k8s.checks['prod-cluster'] = {ok: false, kind, raw, server: 'https://10.0.0.11:6443', server_version: ''}
    await openKubernetes({remember: 'prod-cluster', be})
    const card = (await screen.findByRole('alert')) as HTMLElement
    expect(within(card).getByRole('heading', {name: title})).toBeInTheDocument()
    expect(within(card).getByText(raw)).toBeInTheDocument()
    expect(screen.queryByRole('tablist', {name: 'Kubernetes views'})).toBeNull()
    delete be.state.k8s.checks['prod-cluster'] // the fault is repaired
    await userEvent.click(within(card).getByRole('button', {name: 'Check again'}))
    expect(await screen.findByRole('tablist', {name: 'Kubernetes views'})).toBeInTheDocument()
    expect(calls(be, 'server.k8sCheck')).toHaveLength(2)
  })

  it('offers another cluster on a failure, and Access host where the login is the problem', async () => {
    const be = makeFake({hosts: [web]})
    be.state.k8s.checks['prod-cluster'] = {ok: false, kind: 'unreachable', raw: 'timeout', server: 'https://10.0.0.11:6443', server_version: ''}
    await openKubernetes({remember: 'prod-cluster', be})
    await screen.findByRole('alert')
    expect(screen.getByText(/prod-cluster at https:\/\/10.0.0.11:6443 did not answer within 8 seconds/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Choose another cluster'}))
    await userEvent.click(await screen.findByRole('radio', {name: /staging/}))
    await userEvent.click(screen.getByRole('button', {name: 'Continue'}))
    await userEvent.click(await screen.findByRole('button', {name: 'Open Kubernetes'}))
    expect(await screen.findByRole('tablist', {name: 'Kubernetes views'})).toBeInTheDocument()
  })

  it('says when the kubeconfig cannot be read, with the raw message', async () => {
    const be = makeFake({hosts: [web]})
    be.state.k8s.contexts = {kubeconfig: '', sudo: false, contexts: [], kind: 'kubeconfig', raw: 'error loading config file "/etc/rancher/k3s/k3s.yaml": permission denied'}
    await openKubernetes({be})
    expect(await screen.findByRole('heading', {name: 'The kubeconfig cannot be read'})).toBeInTheDocument()
    expect(screen.getByText(/permission denied/)).toBeInTheDocument()
    expect(calls(be, 'server.k8sCheck')).toHaveLength(0)
  })

  it('changes the cluster from the bar of the tab and remembers the new one', async () => {
    const be = makeFake({hosts: [web]})
    await openKubernetes({remember: 'prod-cluster', be})
    await screen.findByRole('tablist', {name: 'Kubernetes views'})
    await userEvent.click(screen.getByRole('combobox', {name: /^Context/}))
    await userEvent.click(await screen.findByRole('option', {name: /staging-cluster/}))
    await waitFor(() => expect(calls(be, 'server.k8sCheck')).toContainEqual(['server.k8sCheck', 'staging-cluster']))
    await screen.findByRole('tablist', {name: 'Kubernetes views'})
    expect(localStorage.getItem('vutty.k8s.context.h1')).toBe('staging-cluster')
    expect(subTab('Pods')).toBeInTheDocument()
  })
})

/** the tab opened without clearing what the browser remembers, and without waiting for the connection to be made */
async function openKeeping(be: ReturnType<typeof makeFake>) {
  render(<Providers be={be}><ServerPanel host={web} visible onShell={vi.fn()} /></Providers>)
  await screen.findByText(/up 41d 0h/)
  await userEvent.click(screen.getByRole('tab', {name: 'Kubernetes'}))
}

describe('Kubernetes: connect flow, what is shown while it looks', () => {
  beforeEach(() => { localStorage.clear() })

  const slowDetect = (be: ReturnType<typeof makeFake>, ms: number) => {
    const detect = be.server.k8sDetect
    be.server.k8sDetect = async s => { await new Promise(r => setTimeout(r, ms)); return detect(s) }
  }

  it('does not draw the list of steps for an answer that comes at once: the answer is shown by itself', async () => {
    const be = makeFake({hosts: [web]})
    be.state.k8s.detect = {found: false, cmd: '', path: '', client_version: '', tried: ['kubectl']}
    await openKubernetes({be})
    expect(screen.queryByText('looking for it …')).toBeNull() // no card that flashes and goes
    expect(await screen.findByRole('heading', {name: 'kubectl was not found on this server'})).toBeInTheDocument()
    expect(screen.queryByText('looking for it …')).toBeNull()
  })

  it('draws the list of steps when a step takes a while', async () => {
    const be = makeFake({hosts: [web]})
    slowDetect(be, 700)
    await openKubernetes({be})
    expect(await screen.findByText('looking for it …')).toBeInTheDocument()
    expect(screen.getByRole('heading', {name: 'Looking for Kubernetes on this server'})).toBeInTheDocument()
    expect(await screen.findByRole('radiogroup', {name: 'Cluster'}, {timeout: 3000})).toBeInTheDocument()
  })

  it('remembers that a host had no kubectl, and forgets it when kubectl is found', async () => {
    const be = makeFake({hosts: [web]})
    be.state.k8s.detect = {found: false, cmd: '', path: '', client_version: '', tried: ['kubectl', 'k3s kubectl']}
    await openKubernetes({be})
    await screen.findByRole('heading', {name: 'kubectl was not found on this server'})
    expect(JSON.parse(localStorage.getItem('vutty.k8s.nokubectl.h1') ?? 'null')).toEqual({tried: ['kubectl', 'k3s kubectl']})
    be.state.k8s.detect = {...be.state.k8s.detect, found: true, cmd: 'kubectl', path: '/usr/bin/kubectl', client_version: 'v1.30.4'}
    await userEvent.click(screen.getByRole('button', {name: 'Check again'}))
    await screen.findByRole('radiogroup', {name: 'Cluster'})
    expect(localStorage.getItem('vutty.k8s.nokubectl.h1')).toBeNull()
  })

  it('says at once that a host had no kubectl last time, while it looks again, and goes on when it is there now', async () => {
    const be = makeFake({hosts: [web]})
    localStorage.setItem('vutty.k8s.nokubectl.h1', JSON.stringify({tried: ['kubectl', 'k3s kubectl']}))
    slowDetect(be, 500) // kubectl has been installed since: found, but not at once
    await openKeeping(be)
    expect(screen.getByRole('heading', {name: 'kubectl was not found on this server'})).toBeInTheDocument() // there from the first moment
    expect(screen.getByText('Tried: kubectl, k3s kubectl')).toBeInTheDocument()
    expect(await screen.findByRole('radiogroup', {name: 'Cluster'}, {timeout: 3000})).toBeInTheDocument() // looked again, found: it goes on
    expect(screen.queryByRole('heading', {name: 'kubectl was not found on this server'})).toBeNull()
    expect(calls(be, 'server.k8sDetect')).toHaveLength(1)
    expect(localStorage.getItem('vutty.k8s.nokubectl.h1')).toBeNull()
  })

  it('keeps saying it when it is still missing, and takes the new list of places tried', async () => {
    const be = makeFake({hosts: [web]})
    localStorage.setItem('vutty.k8s.nokubectl.h1', JSON.stringify({tried: ['kubectl']}))
    be.state.k8s.detect = {found: false, cmd: '', path: '', client_version: '', tried: ['kubectl', 'k3s kubectl', 'microk8s kubectl']}
    await openKeeping(be)
    expect(await screen.findByText('Tried: kubectl, k3s kubectl, microk8s kubectl')).toBeInTheDocument()
    expect(JSON.parse(localStorage.getItem('vutty.k8s.nokubectl.h1') ?? 'null')).toEqual({tried: ['kubectl', 'k3s kubectl', 'microk8s kubectl']})
  })

  it('ignores what it cannot read of what it remembered', async () => {
    const be = makeFake({hosts: [web]})
    localStorage.setItem('vutty.k8s.nokubectl.h1', '{"tried": "not a list"}')
    await openKeeping(be)
    expect(await screen.findByRole('radiogroup', {name: 'Cluster'})).toBeInTheDocument()
  })
})
