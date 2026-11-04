import {fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {makeFake, Providers} from '../../test/fakeBackend'
import type {Cluster} from '../types'

vi.mock('../terminal/TerminalPane', () => ({TerminalPane: () => <div data-testid="terminal" />}))

import App from '../../App'

const cluster = (o: Partial<Cluster> = {}): Cluster => ({id: 'c1', label: 'prod-eks', env: 'prod', context: 'prod', server: 'https://10.0.0.11:6443', created_at: '2026-11-01T10:00:00Z', updated_at: '2026-11-01T10:00:00Z', ...o})
const calls = (be: ReturnType<typeof makeFake>, n: string) => be.state.calls.filter(c => c[0] === n)

async function open(clusters: Cluster[] = [cluster(), cluster({id: 'c2', label: 'dev-kind', env: 'dev', context: 'kind', server: 'https://127.0.0.1:41233'})]) {
  const be = makeFake({clusters})
  render(<Providers be={be}><App /></Providers>)
  await userEvent.type(await screen.findByLabelText('Master password'), be.state.password)
  await userEvent.click(screen.getByRole('button', {name: 'Unlock'}))
  await screen.findByRole('list', {name: 'Hosts'})
  await userEvent.click(within(screen.getByRole('navigation', {name: 'Main'})).getByText('Kubernetes'))
  await screen.findByRole('list', {name: 'Clusters'})
  return be
}
const card = (name: string) => screen.getByText(name, {selector: '.label'}).closest('[role=listitem]') as HTMLElement

beforeEach(() => { localStorage.clear() })
afterEach(() => vi.restoreAllMocks())

describe('Clusters page', () => {
  it('lists the clusters with their address, and marks only the production one', async () => {
    await open()
    expect(within(card('prod-eks')).getByText('PROD')).toBeInTheDocument()
    expect(within(card('prod-eks')).getByText('https://10.0.0.11:6443')).toBeInTheDocument()
    expect(within(card('prod-eks')).getByText(/context prod/)).toBeInTheDocument()
    expect(within(card('dev-kind')).queryByText('PROD')).toBeNull()
    expect(screen.getByText(/stored encrypted in your vault and is never shown again/)).toBeInTheDocument()
  })

  it('says what to do when there is none yet', async () => {
    await open([])
    expect(screen.getByText(/No clusters yet/)).toBeInTheDocument()
  })

  it('asks before it deletes, and does nothing when declined', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const be = await open()
    await userEvent.click(screen.getByRole('button', {name: 'Delete prod-eks'}))
    await waitFor(() => expect(confirm).toHaveBeenCalledTimes(1))
    expect(confirm.mock.calls[0][0]).toMatch(/Delete the cluster "prod-eks"\? Its kubeconfig is removed from the vault\./)
    expect(calls(be, 'clusters.remove')).toHaveLength(0)
    expect(card('prod-eks')).toBeInTheDocument()
    confirm.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Delete prod-eks'}))
    await waitFor(() => expect(screen.queryByText('prod-eks', {selector: '.label'})).toBeNull())
    expect(calls(be, 'clusters.remove')).toEqual([['clusters.remove', 'c1']])
  })
})

describe('Add cluster', () => {
  it('reads the picked file, shows its contexts, the ones refused and the login program, then saves with the path (never the text)', async () => {
    const be = await open([])
    await userEvent.click(screen.getByRole('button', {name: 'Add cluster'}))
    await userEvent.click(screen.getByRole('button', {name: 'Choose file…'}))
    expect(await screen.findByRole('radiogroup', {name: 'Context'})).toBeInTheDocument()
    expect(calls(be, 'clusters.preview')).toEqual([['clusters.preview', '/tmp/picked', '']])
    expect(screen.getByRole('radio', {name: /prod/})).toHaveAttribute('aria-checked', 'true') // the current one of the file
    expect(screen.getByLabelText('Login program')).toHaveTextContent('aws eks get-token --cluster-name prod')
    expect(screen.getByText(/A login program runs on this computer to sign in/)).toBeInTheDocument()
    expect(screen.getByText(/legacy: this kubeconfig reads a token from a file/)).toBeInTheDocument() // refused, with the reason
    await userEvent.click(screen.getByRole('radio', {name: /eks/}))
    await userEvent.type(screen.getByLabelText('Cluster name'), 'prod-eks')
    await userEvent.click(screen.getByRole('button', {name: 'Add'}))
    await waitFor(() => expect(calls(be, 'clusters.add')).toHaveLength(1))
    expect(calls(be, 'clusters.add')[0][1]).toMatchObject({label: 'prod-eks', path: '/tmp/picked', kubeconfig: '', context: 'eks'})
    expect(await screen.findByText('prod-eks', {selector: '.label'})).toBeInTheDocument()
  })

  it('warns about a server whose certificate is not checked', async () => {
    const be = makeFake()
    be.state.preview.contexts[0] = {...be.state.preview.contexts[0], insecure: true}
    render(<Providers be={be}><App /></Providers>)
    await userEvent.type(await screen.findByLabelText('Master password'), be.state.password)
    await userEvent.click(screen.getByRole('button', {name: 'Unlock'}))
    await screen.findByRole('list', {name: 'Hosts'})
    await userEvent.click(within(screen.getByRole('navigation', {name: 'Main'})).getByText('Kubernetes'))
    await userEvent.click(screen.getByRole('button', {name: 'Add cluster'}))
    await userEvent.click(screen.getByRole('button', {name: 'Choose file…'}))
    expect(await screen.findByText(/The certificate of this server is not checked/)).toBeInTheDocument()
  })

  it('falls back to pasted text when the system has no file dialog', async () => {
    const be = await open([])
    be.system.pickFile = async () => { throw new Error('no dialog') }
    await userEvent.click(screen.getByRole('button', {name: 'Add cluster'}))
    await userEvent.click(screen.getByRole('button', {name: 'Choose file…'}))
    await userEvent.type(await screen.findByLabelText('Kubeconfig text'), 'apiVersion: v1')
    await userEvent.click(screen.getByRole('button', {name: 'Read the text'}))
    await screen.findByRole('radiogroup', {name: 'Context'})
    expect(calls(be, 'clusters.preview')).toEqual([['clusters.preview', '', 'apiVersion: v1']])
    await userEvent.click(screen.getByRole('button', {name: 'Add'}))
    await waitFor(() => expect(calls(be, 'clusters.add')).toHaveLength(1))
    expect(calls(be, 'clusters.add')[0][1]).toMatchObject({path: '', kubeconfig: 'apiVersion: v1'})
  })

  it('makes a group right in the form, even the first one, and puts the cluster in it', async () => {
    const be = await open([])
    be.system.pickFile = async () => { throw new Error('no dialog') }
    await userEvent.click(screen.getByRole('button', {name: 'Add cluster'}))
    await userEvent.click(screen.getByRole('button', {name: 'Choose file…'}))
    await userEvent.type(await screen.findByLabelText('Kubeconfig text'), 'apiVersion: v1')
    await userEvent.click(screen.getByRole('button', {name: 'Read the text'}))
    await screen.findByRole('radiogroup', {name: 'Context'})
    expect(be.state.groups).toHaveLength(0) // there is no group yet: the choice is still there
    await userEvent.click(screen.getByRole('combobox', {name: /^Group,/}))
    await userEvent.click(await screen.findByRole('option', {name: 'New group…'}))
    await userEvent.click(screen.getByRole('button', {name: 'Create'})) // no name yet
    expect(await screen.findByText('A group needs a name.')).toBeInTheDocument()
    expect(be.state.groups).toHaveLength(0)
    await userEvent.type(screen.getByLabelText('Group name'), 'Gitec VN{Enter}')
    await waitFor(() => expect(be.state.groups).toHaveLength(1))
    expect(be.state.groups[0]).toMatchObject({name: 'Gitec VN'})
    expect(screen.getByRole('combobox', {name: /^Group,/})).toHaveTextContent('Gitec VN') // selected, and the box is gone
    expect(screen.queryByLabelText('Group name')).not.toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Add'}))
    await waitFor(() => expect(calls(be, 'clusters.add')).toHaveLength(1))
    expect(calls(be, 'clusters.add')[0][1]).toMatchObject({group_id: be.state.groups[0].id})
  })

  it('shows why a file is refused and cannot be saved', async () => {
    const be = await open([])
    be.state.previewError = 'this is not a kubeconfig'
    await userEvent.click(screen.getByRole('button', {name: 'Add cluster'}))
    await userEvent.click(screen.getByRole('button', {name: 'Choose file…'}))
    expect(await screen.findByText('this is not a kubeconfig')).toBeInTheDocument()
    expect(screen.getByRole('button', {name: 'Add'})).toBeDisabled()
  })

  it('replaces the kubeconfig of a cluster and says that approvals are dropped', async () => {
    const be = await open()
    await userEvent.click(screen.getByRole('button', {name: 'Replace the kubeconfig of prod-eks'}))
    expect(screen.getByText(/not approved for the new one/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Choose file…'}))
    await screen.findByRole('radiogroup', {name: 'Context'})
    await userEvent.click(screen.getByRole('button', {name: 'Replace'}))
    await waitFor(() => expect(calls(be, 'clusters.replaceKubeconfig')).toEqual([['clusters.replaceKubeconfig', 'c1', '/tmp/picked', '']]))
  })

  it('has Replace kubeconfig in the edit form too, which opens the panel for a new file', async () => {
    const be = await open()
    await userEvent.click(screen.getByRole('button', {name: 'Edit prod-eks'}))
    expect(await screen.findByText(/Stored encrypted in the vault and never shown again/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Replace the kubeconfig'}))
    expect(await screen.findByText(/not approved for the new one/)).toBeInTheDocument() // the panel of a new kubeconfig
    await userEvent.click(screen.getByRole('button', {name: 'Choose file…'}))
    await screen.findByRole('radiogroup', {name: 'Context'})
    await userEvent.click(screen.getByRole('button', {name: 'Replace'}))
    await waitFor(() => expect(calls(be, 'clusters.replaceKubeconfig')).toHaveLength(1))
    expect(calls(be, 'clusters.replaceKubeconfig')[0][1]).toBe('c1')
  })

  it('shows what the stored kubeconfig holds (contexts, addresses, how each signs in), nothing secret', async () => {
    const be = await open()
    await userEvent.click(screen.getByRole('button', {name: 'Edit prod-eks'}))
    expect(await screen.findByText('https://ABC.gr7.eu-west-1.eks.amazonaws.com')).toBeInTheDocument()
    expect(screen.getByText(/login program: aws eks get-token --cluster-name prod/)).toBeInTheDocument()
    expect(calls(be, 'clusters.describe')).toHaveLength(1)
    expect(document.body.textContent).not.toMatch(/Kubeconfig text|BEGIN CERTIFICATE|Bearer/)
  })

  it('lists the login programs that were approved, and takes an approval back', async () => {
    const be = await open([cluster({approved_exec: ['aws eks get-token --cluster-name prod', '/opt/bin/get-token']})])
    await userEvent.click(screen.getByRole('button', {name: 'Edit prod-eks'}))
    expect(await screen.findByText('aws eks get-token --cluster-name prod')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Revoke /opt/bin/get-token'}))
    await waitFor(() => expect(calls(be, 'clusters.revokeExec')).toEqual([['clusters.revokeExec', 'c1', '/opt/bin/get-token']]))
    await waitFor(() => expect(screen.queryByText('/opt/bin/get-token')).toBeNull())
    expect(screen.getByText('aws eks get-token --cluster-name prod')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Revoke aws eks get-token --cluster-name prod'}))
    expect(await screen.findByText('No login program was approved for this cluster.')).toBeInTheDocument()
  })

  it('edits name, environment and group', async () => {
    const be = await open()
    await userEvent.click(screen.getByRole('button', {name: 'Edit dev-kind'}))
    const name = screen.getByLabelText('Cluster name')
    await userEvent.clear(name)
    await userEvent.type(name, 'dev-local')
    await userEvent.click(screen.getByRole('button', {name: 'Save'}))
    await waitFor(() => expect(calls(be, 'clusters.update')).toHaveLength(1))
    expect(calls(be, 'clusters.update')[0][1]).toMatchObject({id: 'c2', label: 'dev-local'})
    expect(await screen.findByText('dev-local', {selector: '.label'})).toBeInTheDocument()
  })
})

describe('Groups on the Kubernetes page, as on the Hosts page', () => {
  const groups = [{id: 'g1', name: 'Sieuviet', sort_order: 0}, {id: 'g2', name: 'Gitec', sort_order: 1}]
  async function openGrouped() {
    const be = makeFake({groups, clusters: [cluster({id: 'a', label: 'prod-eks', group_id: 'g1'}), cluster({id: 'b', label: 'staging', env: 'staging', group_id: 'g1'}), cluster({id: 'c', label: 'dev-kind', env: 'dev', group_id: undefined})]})
    render(<Providers be={be}><App /></Providers>)
    await userEvent.type(await screen.findByLabelText('Master password'), be.state.password)
    await userEvent.click(screen.getByRole('button', {name: 'Unlock'}))
    await screen.findByRole('list', {name: 'Hosts'})
    await userEvent.click(within(screen.getByRole('navigation', {name: 'Main'})).getByText('Kubernetes'))
    await screen.findByRole('list', {name: 'Clusters'})
    return be
  }
  const labels = () => screen.queryAllByText(/.*/, {selector: '[role=listitem] .label'}).map(e => e.firstChild?.textContent) // without the PROD badge

  it('shows group cards with how many clusters each holds, and every cluster below them', async () => {
    await openGrouped()
    const sieuviet = screen.getByRole('button', {name: 'Open group Sieuviet'})
    expect(within(sieuviet).getByText('2 Clusters')).toBeInTheDocument()
    expect(within(screen.getByRole('button', {name: 'Open group Gitec'})).getByText('0 Clusters')).toBeInTheDocument()
    expect(labels()).toEqual(['dev-kind', 'prod-eks', 'staging'])
  })

  it('opens a group: a path to come back, and only its clusters', async () => {
    await openGrouped()
    await userEvent.click(screen.getByRole('button', {name: 'Open group Sieuviet'}))
    expect(labels()).toEqual(['prod-eks', 'staging'])
    const path = screen.getByRole('navigation', {name: 'Group path'})
    expect(within(path).getByText('Sieuviet')).toBeInTheDocument()
    await userEvent.click(within(path).getByRole('button', {name: 'All clusters'}))
    expect(labels()).toHaveLength(3)
    await userEvent.click(screen.getByRole('button', {name: 'Open group Gitec'}))
    expect(await screen.findByText('This group has no clusters yet.')).toBeInTheDocument()
  })

  it('searches every group at once and leaves the group cards out while it does', async () => {
    await openGrouped()
    await userEvent.click(screen.getByRole('button', {name: 'Open group Gitec'}))
    await userEvent.type(screen.getByRole('textbox', {name: /Search/}), 'staging')
    expect(labels()).toEqual(['staging'])
    expect(screen.queryByRole('button', {name: 'Open group Sieuviet'})).not.toBeInTheDocument()
  })

  it('moves a cluster into a group when its card is dropped on the group card, and out again on All clusters', async () => {
    const be = await openGrouped()
    const drop = (to: HTMLElement, id: string) => fireEvent.drop(to, {dataTransfer: {types: ['application/x-vutty-cluster'], getData: () => id}})
    drop(screen.getByRole('button', {name: 'Open group Gitec'}), 'c')
    await waitFor(() => expect(calls(be, 'clusters.update')).toHaveLength(1))
    expect(calls(be, 'clusters.update')[0][1]).toMatchObject({id: 'c', group_id: 'g2'})
    await userEvent.click(screen.getByRole('button', {name: 'Open group Sieuviet'}))
    drop(within(screen.getByRole('navigation', {name: 'Group path'})).getByRole('button', {name: 'All clusters'}), 'a')
    await waitFor(() => expect(calls(be, 'clusters.update')).toHaveLength(2))
    expect(calls(be, 'clusters.update')[1][1]).toMatchObject({id: 'a', group_id: undefined})
  })

  it('makes and deletes groups with the same dialogs as Hosts, and a deleted group does not take its clusters with it', async () => {
    const confirm = vi.spyOn(window, 'confirm').mockReturnValue(true)
    const be = await openGrouped()
    await userEvent.click(screen.getByRole('button', {name: 'New group'}))
    expect((await screen.findAllByText('New group')).length).toBeGreaterThan(1) // the button and the title of the panel it opened
    await userEvent.click(screen.getByRole('button', {name: 'Delete group Sieuviet'}))
    await waitFor(() => expect(confirm).toHaveBeenCalled())
    expect(confirm.mock.calls.at(-1)![0]).toMatch(/The 2 clusters in it are kept, without a group\./)
    await waitFor(() => expect(be.state.groups.map(g => g.id)).toEqual(['g2']))
    expect(calls(be, 'clusters.update').map(c => (c[1] as Cluster).id).sort()).toEqual(['a', 'b'])
    expect(calls(be, 'clusters.update').every(c => (c[1] as Cluster).group_id === undefined)).toBe(true)
    expect(labels()).toEqual(['dev-kind', 'prod-eks', 'staging']) // still there
  })
})

describe('a cluster tab', () => {
  async function openProd(be?: ReturnType<typeof makeFake>) {
    const b = be ?? makeFake({clusters: [cluster()]})
    b.state.k8s.detect = {...b.state.k8s.detect, direct: true, cmd: 'kubeconfig', path: '', client_version: ''}
    render(<Providers be={b}><App /></Providers>)
    await userEvent.type(await screen.findByLabelText('Master password'), b.state.password)
    await userEvent.click(screen.getByRole('button', {name: 'Unlock'}))
    await screen.findByRole('list', {name: 'Hosts'})
    await userEvent.click(within(screen.getByRole('navigation', {name: 'Main'})).getByText('Kubernetes'))
    await userEvent.click(await screen.findByRole('button', {name: 'Open prod-eks'}))
    return b
  }

  it('opens the session, skips the kubectl step, and shows "direct" and no SSH-only buttons', async () => {
    const be = await openProd()
    expect(await screen.findByRole('tab', {name: /^prod-eks$/})).toBeInTheDocument()
    expect(calls(be, 'clusters.open')).toEqual([['clusters.open', 'c1']])
    expect(await screen.findByRole('radiogroup', {name: 'Cluster'})).toBeInTheDocument() // the choice of a context; no "looking for kubectl"
    expect(screen.queryByText('kubectl was not found on this server')).toBeNull()
    expect(screen.queryByRole('button', {name: 'Access host'})).toBeNull()
    expect(within(screen.getByRole('region', {name: 'Cluster prod-eks'})).getByText('PROD')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('radio', {name: /prod-cluster/}))
    await userEvent.click(screen.getByRole('button', {name: 'Continue'}))
    await userEvent.click(await screen.findByRole('button', {name: 'Open Kubernetes'}))
    expect(await screen.findByText('server v1.30.2 · direct')).toBeInTheDocument()
  })

  it('opens a shell in a pod as a terminal tab of its own, and has no Port-forward, with the reason in the tooltip', async () => {
    const be = await openProd()
    await userEvent.click(await screen.findByRole('radio', {name: /prod-cluster/}))
    await userEvent.click(screen.getByRole('button', {name: 'Continue'}))
    await userEvent.click(await screen.findByRole('button', {name: 'Open Kubernetes'}))
    const views = within(await screen.findByRole('tablist', {name: 'Kubernetes views'}))
    await userEvent.click(views.getByRole('tab', {name: 'Network'}))
    const forwards = await screen.findAllByRole('button', {name: /^Port-forward /})
    for (const b of forwards) { expect(b).toBeDisabled(); expect(b).toHaveAttribute('title', expect.stringMatching(/Needs kubectl on a server/)) }
    await userEvent.click(views.getByRole('tab', {name: 'Pods'}))
    const shells = (await screen.findAllByRole('button', {name: /^Shell/})).filter(b => !(b as HTMLButtonElement).disabled && b.textContent === 'Shell') // a pod of one container opens at once
    expect(shells.length).toBeGreaterThan(0) // no kubectl is needed: the shell goes over the cluster's API
    await userEvent.click(shells[0])
    await waitFor(() => expect(calls(be, 'server.k8sShellSession')).toHaveLength(1))
    expect(calls(be, 'server.k8sShellCommand')).toHaveLength(0) // that is the line for a host
    expect(await screen.findByTestId('terminal')).toBeInTheDocument() // the terminal tab opened
  })

  it('reads the log of a pod in the Logs section, which offers pods only and asks the host for nothing', async () => {
    const be = await openProd()
    await userEvent.click(await screen.findByRole('radio', {name: /prod-cluster/}))
    await userEvent.click(screen.getByRole('button', {name: 'Continue'}))
    await userEvent.click(await screen.findByRole('button', {name: 'Open Kubernetes'}))
    await userEvent.click(within(await screen.findByRole('tablist', {name: 'Kubernetes views'})).getByRole('tab', {name: 'Pods'}))
    await userEvent.click((await screen.findAllByRole('button', {name: /^Logs/}))[0])
    expect(await screen.findByRole('tab', {name: 'Logs', selected: true})).toBeInTheDocument()
    await waitFor(() => expect(calls(be, 'server.k8sLogs').length).toBeGreaterThan(0))
    expect(calls(be, 'server.services')).toHaveLength(0)
    expect(calls(be, 'server.containers')).toHaveLength(0)
    expect(calls(be, 'server.logs')).toHaveLength(0)
  })

  it('lists every container of the pod in the Logs section, also for a login that may not list the whole cluster', async () => {
    const be = makeFake({clusters: [cluster()]})
    be.state.k8s.detect = {...be.state.k8s.detect, direct: true, cmd: 'kubeconfig', path: '', client_version: ''}
    const pods = be.server.k8sPods
    be.server.k8sPods = async (sid, c, n) => { if (!n) throw new Error('pods is forbidden: cannot list at the cluster scope'); return pods(sid, c, n) } // only a namespace may be listed
    be.server.k8sNamespaces = async () => { throw new Error('namespaces is forbidden: cannot list at the cluster scope') }
    render(<Providers be={be}><App /></Providers>)
    await userEvent.type(await screen.findByLabelText('Master password'), be.state.password)
    await userEvent.click(screen.getByRole('button', {name: 'Unlock'}))
    await screen.findByRole('list', {name: 'Hosts'})
    await userEvent.click(within(screen.getByRole('navigation', {name: 'Main'})).getByText('Kubernetes'))
    await userEvent.click(await screen.findByRole('button', {name: 'Open prod-eks'}))
    await userEvent.click(await screen.findByRole('radio', {name: /prod-cluster/}))
    await userEvent.click(screen.getByRole('button', {name: 'Continue'}))
    await userEvent.click(await screen.findByRole('button', {name: 'Open Kubernetes'}))
    await userEvent.click(within(await screen.findByRole('tablist', {name: 'Kubernetes views'})).getByRole('tab', {name: 'Pods'}))
    await userEvent.click(await screen.findByRole('button', {name: 'Add namespace'}))
    await userEvent.type(screen.getByRole('textbox', {name: 'Namespace name'}), 'shop{Enter}')
    const row = (await screen.findByText('web-6b7d9-m4zwn')).closest('tr') as HTMLElement
    await userEvent.click(within(row).getByRole('button', {name: /^Logs/}))
    await userEvent.click(await screen.findByRole('combobox', {name: /^Container,/}))
    const names = (await screen.findAllByRole('option')).map(o => o.textContent)
    expect(names).toEqual(expect.arrayContaining(['Default container', 'web', 'nginx-sidecar', 'init-config (init)']))
    expect(calls(be, 'server.k8sPod').some(c => c[2] === 'shop' && c[3] === 'web-6b7d9-m4zwn')).toBe(true) // from the pod itself
  })

  it('shows one tab for a cluster: opening it again goes to it', async () => {
    const be = await openProd()
    await screen.findByRole('tab', {name: /^prod-eks$/})
    await userEvent.click(screen.getByRole('tab', {name: 'Vaults'}))
    await userEvent.click(await screen.findByRole('button', {name: 'Open prod-eks'}))
    expect(screen.getAllByRole('tab', {name: /^prod-eks$/})).toHaveLength(1)
    expect(calls(be, 'clusters.open')).toHaveLength(1)
  })

  it('closes the session when the tab closes', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const be = await openProd()
    await screen.findByRole('radiogroup', {name: 'Cluster'})
    await userEvent.click(screen.getByRole('button', {name: /Close prod-eks$/}))
    await waitFor(() => expect(calls(be, 'server.close')).toEqual([['server.close', 'kube1']]))
  })

  it('asks to approve a login program, approves it, and opens the session again', async () => {
    const be = makeFake({clusters: [cluster()]})
    const cmd = 'aws eks get-token --cluster-name prod'
    be.state.k8s.contexts.contexts = be.state.k8s.contexts.contexts.slice(0, 1)
    be.state.k8s.checks['prod-cluster'] = {ok: false, kind: 'approval', raw: cmd, server: 'https://10.0.0.11:6443', server_version: ''}
    const approve = be.clusters.approveExec
    be.clusters.approveExec = async (id, c) => { be.state.k8s.checks['prod-cluster'] = {ok: true, kind: '', raw: '', server: 'https://10.0.0.11:6443', server_version: 'v1.30.2'}; return approve(id, c) }
    await openProd(be)
    expect(await screen.findByRole('heading', {name: 'A program wants to run on this computer'})).toBeInTheDocument()
    expect(screen.getByLabelText('Command line')).toHaveTextContent(cmd)
    expect(screen.getByText(/runs the program below on this computer/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Approve and run'}))
    await waitFor(() => expect(calls(be, 'clusters.approveExec')).toEqual([['clusters.approveExec', 'c1', cmd]]))
    await waitFor(() => expect(calls(be, 'clusters.open')).toHaveLength(2)) // the backend learns the approval when a session opens
    expect(await screen.findByRole('button', {name: 'Open Kubernetes'})).toBeInTheDocument()
  })

  it('does not approve when cancelled', async () => {
    const be = makeFake({clusters: [cluster()]})
    be.state.k8s.contexts.contexts = be.state.k8s.contexts.contexts.slice(0, 1)
    be.state.k8s.checks['prod-cluster'] = {ok: false, kind: 'approval', raw: 'aws eks get-token', server: '', server_version: ''}
    await openProd(be)
    await userEvent.click(await screen.findByRole('button', {name: 'Cancel'}))
    expect(await screen.findByText('The login program was not approved, so the cluster cannot be asked.')).toBeInTheDocument()
    expect(calls(be, 'clusters.approveExec')).toHaveLength(0)
  })
})
