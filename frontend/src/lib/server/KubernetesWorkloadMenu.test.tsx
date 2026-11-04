import {render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {calls, openOn, rowOf, web, type Fake} from '../../test/k8sKit'
import {makeFake, Providers} from '../../test/fakeBackend'

vi.mock('../terminal/TerminalPane', () => ({TerminalPane: () => <div data-testid="terminal" />}))

import App from '../../App'
import type {Cluster} from '../types'

beforeEach(() => { vi.spyOn(window, 'confirm').mockReturnValue(true) })
afterEach(() => { vi.restoreAllMocks() })

const CTX = 'prod-cluster'

/** a host (kubectl over SSH) or a cluster opened with a kubeconfig, on the Workloads view */
async function workloads(o: {direct?: boolean; rights?: Record<string, boolean>; be?: Fake} = {}) {
  const be = o.be ?? makeFake({hosts: [web]})
  if (o.direct) be.state.k8s.detect = {...be.state.k8s.detect, direct: true, cmd: 'kubeconfig', path: '', client_version: ''}
  const r = await openOn('Workloads', {be, rights: o.rights})
  await screen.findByText('web')
  return {...r, be}
}
const menuOf = async (name: string) => {
  await userEvent.click(within(rowOf(name)).getByRole('button', {name: `More actions for ${name}`}))
  return screen.findByRole('menu', {name: `Actions for ${name}`})
}
const item = (menu: HTMLElement, name: RegExp) => within(menu).getByRole('menuitem', {name})
const pick = async (name: string, what: RegExp) => { const m = await menuOf(name); await userEvent.click(item(m, what)) }

describe('the menu of a workload, as Rancher has it', () => {
  it('offers Execute shell, Show configuration, Edit config, Add sidecar, Edit YAML and Download YAML on a Deployment, a StatefulSet and a DaemonSet, and nothing on a Job', async () => {
    await workloads({direct: true})
    for (const name of ['web']) {
      const m = await menuOf(name)
      for (const label of [/^Execute shell/, /^Show configuration/, /^Edit config/, /^Add sidecar/, /^Edit YAML/, /^Download YAML/]) expect(item(m, label)).toBeInTheDocument()
      await userEvent.keyboard('{Escape}')
    }
    await userEvent.click(screen.getByRole('tab', {name: /StatefulSets/}))
    expect(await menuOf('redis')).toBeInTheDocument()
    await userEvent.keyboard('{Escape}')
    await userEvent.click(screen.getByRole('tab', {name: /DaemonSets/}))
    expect(await menuOf('node-exporter')).toBeInTheDocument()
    await userEvent.keyboard('{Escape}')
    await userEvent.click(screen.getByRole('tab', {name: /^Jobs/}))
    expect(screen.queryByRole('button', {name: /^More actions for/})).not.toBeInTheDocument()
  })

  it('keeps Restart, Pause and Rollback, named so a Rancher user finds them', async () => {
    await workloads()
    expect(within(rowOf('web')).getByRole('button', {name: 'Restart web'})).toHaveAttribute('title', expect.stringMatching(/^Redeploy/))
    await userEvent.click(within(rowOf('api')).getByRole('button', {name: 'api'}))
    expect(await screen.findByRole('button', {name: 'Pause'})).toHaveAttribute('title', expect.stringMatching(/^Pause orchestration/))
    expect(screen.getAllByRole('button', {name: /^Roll back to/})[0]).toHaveAttribute('title', expect.stringMatching(/^Rollback/))
    expect(screen.getByRole('button', {name: 'More actions for api'})).toBeInTheDocument() // the menu is in the detail too
  })
})

describe('what the login may do decides what is on', () => {
  it('locks Edit YAML with the reason when the login may not update the workload, and leaves the rest', async () => {
    await workloads({direct: true, rights: {edit: false}})
    const m = await menuOf('web')
    expect(item(m, /^Edit YAML/)).toBeDisabled()
    expect(item(m, /^Edit YAML/)).toHaveAttribute('title', 'This login may not edit this workload')
    for (const label of [/^Edit config/, /^Add sidecar/, /^Show configuration/, /^Download YAML/]) expect(item(m, label)).toBeEnabled()
  })

  it('locks Edit config and Add sidecar when the login may not patch the workload, but not reading it', async () => {
    await workloads({direct: true, rights: {restart: false}})
    const m = await menuOf('web')
    for (const label of [/^Edit config/, /^Add sidecar/]) { expect(item(m, label)).toBeDisabled(); expect(item(m, label)).toHaveAttribute('title', 'This login may not change this workload') }
    for (const label of [/^Show configuration/, /^Download YAML/, /^Edit YAML/]) expect(item(m, label)).toBeEnabled()
  })

  it('locks Execute shell when the login may not exec', async () => {
    await workloads({direct: true, rights: {exec: false}})
    const m = await menuOf('web')
    expect(item(m, /^Execute shell/)).toBeDisabled()
    expect(item(m, /^Execute shell/)).toHaveAttribute('title', 'This login may not open a shell in pods')
  })

  it('on a host, locks the changes with the reason but keeps Show configuration, Download and Execute shell', async () => {
    await workloads()
    const m = await menuOf('web')
    for (const label of [/^Edit config/, /^Add sidecar/, /^Edit YAML/]) {
      expect(item(m, label)).toBeDisabled()
      expect(item(m, label)).toHaveAttribute('title', 'Needs a cluster opened with a kubeconfig: on a host use kubectl in a terminal')
    }
    for (const label of [/^Show configuration/, /^Download YAML/, /^Execute shell/]) expect(item(m, label)).toBeEnabled()
  })
})

describe('Execute shell', () => {
  it('asks for the pod and the container when there are several, and opens it like the Shell of a pod (a host: kubectl in an SSH tab)', async () => {
    const {be, onShell} = await workloads()
    await pick('web', /^Execute shell/)
    const dialog = await screen.findByRole('dialog', {name: 'Execute shell in web'})
    expect(within(dialog).getByText('web-6b7d9-m4zwn')).toBeInTheDocument()
    expect(within(dialog).queryByRole('button', {name: /init-config/})).not.toBeInTheDocument() // an init container has nothing to enter
    await userEvent.click(within(dialog).getByRole('button', {name: 'Open a shell in web-6b7d9-q8t2c, container nginx-sidecar'}))
    await waitFor(() => expect(calls(be, 'server.k8sShellCommand')).toEqual([['server.k8sShellCommand', CTX, 'shop', 'web-6b7d9-q8t2c', 'nginx-sidecar']]))
    expect(onShell).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('dialog', {name: 'Execute shell in web'})).not.toBeInTheDocument()
  })

  it('opens at once when there is one running pod of one container', async () => {
    const {be, onShell} = await workloads()
    await userEvent.click(screen.getByRole('tab', {name: /StatefulSets/}))
    await pick('redis', /^Execute shell/)
    await waitFor(() => expect(calls(be, 'server.k8sShellCommand')).toEqual([['server.k8sShellCommand', CTX, 'shop', 'redis-0', 'redis']]))
    expect(onShell).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })

  it('says so when no pod is running', async () => {
    const {be} = await workloads()
    await pick('api', /^Execute shell/) // its only pod is in CrashLoopBackOff
    expect(await screen.findByText('No running pod of api was found.')).toBeInTheDocument()
    expect(calls(be, 'server.k8sShellCommand')).toHaveLength(0)
  })

  it('does not take a pod of another workload whose name starts the same', async () => {
    const be = makeFake({hosts: [web]})
    be.state.k8s.pods = [...be.state.k8s.pods, {...be.state.k8s.pods[3], name: 'web-extra-1', owner: 'ReplicaSet web-extra-5c ← Deployment web-extra'}]
    await workloads({be})
    await pick('web', /^Execute shell/)
    const dialog = await screen.findByRole('dialog', {name: 'Execute shell in web'})
    expect(within(dialog).queryByText('web-extra-1')).not.toBeInTheDocument()
  })
})

describe('Show configuration', () => {
  it('lists every container with its image, variables (from a Secret as such), resources and mounts', async () => {
    const {be} = await workloads()
    await pick('api', /^Show configuration/)
    const dialog = await screen.findByRole('dialog', {name: 'Configuration of api'})
    await within(dialog).findByRole('region', {name: 'api'})
    expect(calls(be, 'server.k8sWorkloadSpec')).toEqual([['server.k8sWorkloadSpec', CTX, 'deployment', 'shop', 'api']])
    expect(within(dialog).getByRole('region', {name: 'init-db'})).toHaveTextContent('init container')
    const api = within(dialog).getByRole('region', {name: 'api'})
    expect(api).toHaveTextContent('registry/api:1.8')
    expect(api).toHaveTextContent('http 3000/TCP')
    expect(api).toHaveTextContent('cpu 250m · memory 256Mi')
    expect(api).toHaveTextContent('config → /etc/api (read only)')
    const env = within(api).getByRole('table', {name: 'Variables of api'})
    expect(env).toHaveTextContent('LOG_LEVEL')
    expect(env).toHaveTextContent('info')
    expect(env).toHaveTextContent('from secret db-credentials:password') // a reference, never its value
    expect(within(dialog).getByText('nginx-sidecar', {selector: 'h3'})).toBeInTheDocument()
    expect(dialog).toHaveTextContent('Service account') // the pod-level facts are there too
  })
})

describe('Edit config', () => {
  async function open() {
    const r = await workloads({direct: true})
    await pick('api', /^Edit config/)
    const dialog = await screen.findByRole('dialog', {name: 'Edit config of api'})
    await within(dialog).findByLabelText('Image')
    return {...r, dialog}
  }

  it('sends one call with only what changed, after asking, and says the pods are replaced', async () => {
    const {be, dialog} = await open()
    const apply = within(dialog).getByRole('button', {name: 'Apply'})
    expect(apply).toBeDisabled() // nothing changed yet
    const confirm = vi.mocked(window.confirm)
    await userEvent.clear(within(dialog).getByLabelText('Image'))
    await userEvent.type(within(dialog).getByLabelText('Image'), 'registry/api:1.9')
    await userEvent.clear(within(dialog).getByLabelText('CPU limit'))
    await userEvent.type(within(dialog).getByLabelText('CPU limit'), '2')
    await userEvent.click(within(dialog).getByRole('button', {name: 'Remove LOG_LEVEL'}))
    await userEvent.click(within(dialog).getByRole('button', {name: 'Add variable'}))
    await userEvent.type(within(dialog).getByLabelText('Variable name'), 'FEATURE')
    await userEvent.type(within(dialog).getByLabelText('Variable value'), 'on')
    expect(apply).toBeEnabled()
    await userEvent.click(apply)
    await waitFor(() => expect(calls(be, 'server.k8sSetContainer')).toHaveLength(1))
    expect(confirm.mock.calls[0][0]).toBe('Change the container api of the Deployment api in shop on prod-cluster? Its pods are replaced by new ones with the new settings.')
    expect(calls(be, 'server.k8sSetContainer')[0]).toEqual(['server.k8sSetContainer', CTX, 'deployment', 'shop', 'api', {
      container: 'api', image: 'registry/api:1.9', limits: {cpu: '2'},
      env: [{name: 'LOG_LEVEL', value: '', remove: true}, {name: 'FEATURE', value: 'on'}],
    }])
    expect(screen.queryByRole('dialog', {name: 'Edit config of api'})).not.toBeInTheDocument()
  })

  it('does nothing when the question is declined', async () => {
    const {be, dialog} = await open()
    vi.mocked(window.confirm).mockReturnValue(false)
    await userEvent.clear(within(dialog).getByLabelText('Memory request'))
    await userEvent.type(within(dialog).getByLabelText('Memory request'), '512Mi')
    await userEvent.click(within(dialog).getByRole('button', {name: 'Apply'}))
    await waitFor(() => expect(window.confirm).toHaveBeenCalledTimes(1))
    expect(calls(be, 'server.k8sSetContainer')).toHaveLength(0)
  })

  it('offers only Remove for a variable that comes from a Secret or a field, and changes a literal one in place', async () => {
    const {be, dialog} = await open()
    expect(within(dialog).queryByLabelText('Value of DB_PASSWORD')).not.toBeInTheDocument()
    expect(within(dialog).getByText('from secret db-credentials:password')).toBeInTheDocument()
    expect(within(dialog).getByText('from field metadata.name')).toBeInTheDocument()
    expect(within(dialog).getByRole('button', {name: 'Remove DB_PASSWORD'})).toBeInTheDocument()
    await userEvent.clear(within(dialog).getByLabelText('Value of LOG_LEVEL'))
    await userEvent.type(within(dialog).getByLabelText('Value of LOG_LEVEL'), 'debug')
    await userEvent.click(within(dialog).getByRole('button', {name: 'Remove DB_PASSWORD'}))
    await userEvent.click(within(dialog).getByRole('button', {name: 'Apply'}))
    await waitFor(() => expect(calls(be, 'server.k8sSetContainer')).toHaveLength(1))
    expect(calls(be, 'server.k8sSetContainer')[0][5]).toEqual({container: 'api', env: [{name: 'LOG_LEVEL', value: 'debug'}, {name: 'DB_PASSWORD', value: '', remove: true}]})
  })

  it('refuses a quantity or a variable name that cannot be, before sending', async () => {
    const {be, dialog} = await open()
    await userEvent.type(within(dialog).getByLabelText('CPU request'), 'x')
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('A quantity looks like')
    expect(within(dialog).getByRole('button', {name: 'Apply'})).toBeDisabled()
    await userEvent.clear(within(dialog).getByLabelText('CPU request'))
    await userEvent.type(within(dialog).getByLabelText('CPU request'), '250m') // back to what it was
    expect(within(dialog).getByRole('button', {name: 'Apply'})).toBeDisabled() // and so nothing changed
    await userEvent.click(within(dialog).getByRole('button', {name: 'Add variable'}))
    await userEvent.type(within(dialog).getByLabelText('Variable name'), '1BAD')
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('A variable name is letters')
    expect(within(dialog).getByRole('button', {name: 'Apply'})).toBeDisabled()
    expect(calls(be, 'server.k8sSetContainer')).toHaveLength(0)
  })

  it('edits an init container with init set, and a cleared limit as ""', async () => {
    const {be, dialog} = await open()
    await userEvent.click(within(dialog).getByRole('combobox', {name: /^Container,/}))
    await userEvent.click(await screen.findByRole('option', {name: 'init-db (init container)'}))
    await userEvent.clear(await within(dialog).findByLabelText('Image'))
    await userEvent.type(within(dialog).getByLabelText('Image'), 'busybox:1.37')
    await userEvent.click(within(dialog).getByRole('button', {name: 'Apply'}))
    await waitFor(() => expect(calls(be, 'server.k8sSetContainer')).toHaveLength(1))
    expect(calls(be, 'server.k8sSetContainer')[0][5]).toEqual({container: 'init-db', init: true, image: 'busybox:1.37'})
  })

  it('shows the cluster\'s own words when it refuses', async () => {
    const {be, dialog} = await open()
    be.state.k8s.fail = 'deployments.apps "api" is forbidden: User "u" cannot patch resource "deployments"'
    await userEvent.clear(within(dialog).getByLabelText('Memory limit'))
    await userEvent.type(within(dialog).getByLabelText('Memory limit'), '1Gi')
    await userEvent.click(within(dialog).getByRole('button', {name: 'Apply'}))
    expect(await screen.findByText(/is forbidden: User "u" cannot patch/)).toBeInTheDocument()
  })
})

describe('Add sidecar', () => {
  async function open() {
    const r = await workloads({direct: true})
    await pick('web', /^Add sidecar/)
    return {...r, dialog: await screen.findByRole('dialog', {name: 'Add a sidecar to web'})}
  }

  it('checks the name like a DNS label before it sends anything', async () => {
    const {be, dialog} = await open()
    const add = within(dialog).getByRole('button', {name: 'Add sidecar'})
    expect(add).toBeDisabled()
    await userEvent.type(within(dialog).getByLabelText('Image'), 'busybox:1.36')
    for (const bad of ['Log Shipper', '-x', 'x_y', 'a'.repeat(64)]) {
      await userEvent.clear(within(dialog).getByLabelText('Container name'))
      await userEvent.type(within(dialog).getByLabelText('Container name'), bad)
      expect(await within(dialog).findByRole('alert')).toHaveTextContent('A container name is lowercase letters')
      expect(add).toBeDisabled()
    }
    expect(calls(be, 'server.k8sAddSidecar')).toHaveLength(0)
  })

  it('asks, then sends the name, the image and the command and arguments one per line', async () => {
    const {be, dialog} = await open()
    await userEvent.type(within(dialog).getByLabelText('Container name'), 'log-shipper')
    await userEvent.type(within(dialog).getByLabelText('Image'), 'fluent/fluent-bit:3')
    await userEvent.type(within(dialog).getByLabelText('Command'), '/fluent-bit/bin/fluent-bit\n')
    await userEvent.type(within(dialog).getByLabelText('Arguments'), '-c\n/etc/fb.conf')
    await userEvent.click(within(dialog).getByRole('button', {name: 'Add sidecar'}))
    await waitFor(() => expect(calls(be, 'server.k8sAddSidecar')).toHaveLength(1))
    expect(vi.mocked(window.confirm).mock.calls[0][0]).toBe('Add the sidecar log-shipper (fluent/fluent-bit:3) to the Deployment web in shop on prod-cluster? Its pods are replaced by new ones that run it too.')
    expect(calls(be, 'server.k8sAddSidecar')[0]).toEqual(['server.k8sAddSidecar', CTX, 'deployment', 'shop', 'web', {name: 'log-shipper', image: 'fluent/fluent-bit:3', command: ['/fluent-bit/bin/fluent-bit'], args: ['-c', '/etc/fb.conf']}])
  })

  it('sends nothing when the question is declined', async () => {
    const {be, dialog} = await open()
    vi.mocked(window.confirm).mockReturnValue(false)
    await userEvent.type(within(dialog).getByLabelText('Container name'), 'x')
    await userEvent.type(within(dialog).getByLabelText('Image'), 'busybox')
    await userEvent.click(within(dialog).getByRole('button', {name: 'Add sidecar'}))
    await waitFor(() => expect(window.confirm).toHaveBeenCalledTimes(1))
    expect(calls(be, 'server.k8sAddSidecar')).toHaveLength(0)
  })
})

describe('Edit YAML', () => {
  async function open(be?: Fake) {
    const r = await workloads({direct: true, be})
    await pick('api', /^Edit YAML/)
    const dialog = await screen.findByRole('dialog', {name: 'Edit YAML of api'})
    const text = await within(dialog).findByLabelText('YAML')
    await waitFor(() => expect(text).toHaveValue(r.be.state.k8s.editable.yaml))
    return {...r, dialog, text: text as HTMLTextAreaElement}
  }
  const apply = (d: HTMLElement) => within(d).getByRole('button', {name: 'Apply'})

  it('locks Apply until a preview of this very text shows changes, and drops the preview when the text changes', async () => {
    const {be, dialog, text} = await open()
    expect(apply(dialog)).toBeDisabled()
    expect(calls(be, 'server.k8sEditable')).toEqual([['server.k8sEditable', CTX, 'deployment', 'shop', 'api']])
    await userEvent.type(text, '# edited')
    expect(apply(dialog)).toBeDisabled() // not previewed yet
    await userEvent.click(within(dialog).getByRole('button', {name: 'Preview'}))
    const diff = await within(dialog).findByLabelText('Changes')
    expect(diff).toHaveTextContent('- replicas: 3')
    expect(diff).toHaveTextContent('+ replicas: 4')
    expect(calls(be, 'server.k8sEditPreview')[0]).toEqual(['server.k8sEditPreview', CTX, 'deployment', 'shop', 'api', be.state.k8s.editable.yaml + '# edited'])
    expect(apply(dialog)).toBeEnabled()
    await userEvent.type(text, '!') // the text is no longer the one that was previewed
    expect(apply(dialog)).toBeDisabled()
    expect(within(dialog).queryByLabelText('Changes')).not.toBeInTheDocument()
    await userEvent.click(within(dialog).getByRole('button', {name: 'Preview'}))
    await waitFor(() => expect(apply(dialog)).toBeEnabled())
    await userEvent.click(apply(dialog))
    await waitFor(() => expect(calls(be, 'server.k8sEditApply')).toHaveLength(1))
    expect(vi.mocked(window.confirm).mock.calls[0][0]).toBe('Replace the Deployment api in shop on prod-cluster with this YAML? 2 lines change; if the pod template changed, the pods are replaced.')
    expect(calls(be, 'server.k8sEditApply')[0]).toEqual(['server.k8sEditApply', CTX, 'deployment', 'shop', 'api', be.state.k8s.editable.yaml + '# edited!'])
    expect(screen.queryByRole('dialog', {name: 'Edit YAML of api'})).not.toBeInTheDocument()
  })

  it('says there is nothing to apply when the preview shows no change', async () => {
    const be = makeFake({hosts: [web]})
    be.state.k8s.preview = {diff: [], changed: 0}
    const {dialog} = await open(be)
    await userEvent.click(within(dialog).getByRole('button', {name: 'Preview'}))
    expect(await within(dialog).findByRole('status')).toHaveTextContent('No changes')
    expect(apply(dialog)).toBeDisabled()
  })

  it('shows what the cluster says about a YAML it refuses, and keeps Apply locked', async () => {
    const be = makeFake({hosts: [web]})
    be.state.k8s.previewError = 'Deployment in version "v1" cannot be handled: strict decoding error: unknown field "spec.replicass"'
    const {dialog} = await open(be)
    await userEvent.click(within(dialog).getByRole('button', {name: 'Preview'}))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('unknown field "spec.replicass"')
    expect(apply(dialog)).toBeDisabled()
  })

  it('says it was changed by someone else, and Reload reads the YAML again', async () => {
    const {be, dialog, text} = await open()
    await userEvent.type(text, '# mine')
    await userEvent.click(within(dialog).getByRole('button', {name: 'Preview'}))
    await waitFor(() => expect(apply(dialog)).toBeEnabled())
    be.state.k8s.fail = 'Operation cannot be fulfilled on deployments.apps "api": the object has been modified; please apply your changes to the latest version and try again'
    await userEvent.click(apply(dialog))
    const alert = await within(dialog).findByText(/Changed by someone else: reload/)
    expect(alert).toBeInTheDocument()
    be.state.k8s.fail = ''
    be.state.k8s.editable = {yaml: 'kind: Deployment\n# theirs\n', version: '900'}
    await userEvent.click(within(dialog).getByRole('button', {name: 'Reload'}))
    await waitFor(() => expect(within(dialog).getByLabelText('YAML')).toHaveValue('kind: Deployment\n# theirs\n'))
    expect(calls(be, 'server.k8sEditable')).toHaveLength(2)
    expect(within(dialog).queryByText(/Changed by someone else/)).not.toBeInTheDocument()
    expect(apply(dialog)).toBeDisabled() // a new text needs a new preview
  })
})

describe('Download YAML', () => {
  it('saves the file and says where, for a host and for a cluster alike', async () => {
    for (const direct of [false, true]) {
      const {be, unmount} = await workloads({direct})
      await pick('api', /^Download YAML/)
      expect(await screen.findByText('Saved to /Users/me/Downloads/shop-api-deployment.yaml')).toBeInTheDocument()
      expect(calls(be, 'server.k8sSaveYAML')).toEqual([['server.k8sSaveYAML', CTX, 'deployment', 'shop', 'api']])
      unmount()
    }
  })
})

/** a cluster opened with a kubeconfig: Execute shell is a terminal tab that talks to the cluster's API */
describe('Execute shell in a cluster opened with a kubeconfig', () => {
  const cluster: Cluster = {id: 'c1', label: 'prod-eks', env: 'prod', context: 'prod', server: 'https://10.0.0.11:6443', created_at: '2026-11-01T10:00:00Z', updated_at: '2026-11-01T10:00:00Z'}

  it('opens the shell of the chosen pod and container as a terminal tab, with no kubectl line', async () => {
    const be = makeFake({clusters: [cluster]})
    be.state.k8s.detect = {...be.state.k8s.detect, direct: true, cmd: 'kubeconfig', path: '', client_version: ''}
    render(<Providers be={be}><App /></Providers>)
    await userEvent.type(await screen.findByLabelText('Master password'), be.state.password)
    await userEvent.click(screen.getByRole('button', {name: 'Unlock'}))
    await screen.findByRole('list', {name: 'Hosts'})
    await userEvent.click(within(screen.getByRole('navigation', {name: 'Main'})).getByText('Kubernetes'))
    await userEvent.click(await screen.findByRole('button', {name: 'Open prod-eks'}))
    await userEvent.click(await screen.findByRole('radio', {name: /prod-cluster/}))
    await userEvent.click(screen.getByRole('button', {name: 'Continue'}))
    await userEvent.click(await screen.findByRole('button', {name: 'Open Kubernetes'}))
    await userEvent.click(within(await screen.findByRole('tablist', {name: 'Kubernetes views'})).getByRole('tab', {name: 'Workloads'}))
    await screen.findByText('web')
    await pick('web', /^Execute shell/)
    await userEvent.click(await screen.findByRole('button', {name: 'Open a shell in web-6b7d9-m4zwn, container web'}))
    await waitFor(() => expect(calls(be, 'server.k8sShellSession')).toEqual([['server.k8sShellSession', CTX, 'shop', 'web-6b7d9-m4zwn', 'web']]))
    expect(calls(be, 'server.k8sShellCommand')).toHaveLength(0)
    expect(await screen.findByTestId('terminal')).toBeInTheDocument()
  })
})
