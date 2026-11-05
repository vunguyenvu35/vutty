import {act, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {calls, goTo, openKubernetes, openOn, rowOf, subTab, web} from '../../test/k8sKit'
import {makeFake} from '../../test/fakeBackend'

beforeEach(() => { vi.spyOn(window, 'confirm').mockReturnValue(true) })
afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

const CTX = 'prod-cluster'
const podRow = (name: string) => rowOf(name)

describe('Overview', () => {
  it('shows the numbers, what needs attention and the namespaces', async () => {
    await openOn('Overview')
    expect(await screen.findByText('41 / 44')).toBeInTheDocument()
    expect(screen.getByText('3 / 3')).toBeInTheDocument()
    expect(screen.getByText('ready · 1 cordoned')).toBeInTheDocument()
    expect(screen.getByText('running · 2 pending · 1 failing')).toBeInTheDocument()
    // Capacity, as in Rancher: what the pods take against what the nodes give, and what is used now when there are metrics
    expect(screen.getByRole('heading', {name: 'Capacity'})).toBeInTheDocument()
    expect(screen.getByRole('meter', {name: 'Pods used 44 of 330'})).toHaveAttribute('aria-valuenow', '13.33')
    expect(screen.getByText('6.96 / 12 cores')).toBeInTheDocument()
    expect(screen.getByRole('meter', {name: 'CPU reserved'})).toHaveAttribute('aria-valuenow', '58')
    expect(screen.getByRole('meter', {name: 'CPU used'})).toHaveAttribute('aria-valuenow', '25.83')
    expect(screen.getByText('37.26 / 46 GiB')).toBeInTheDocument()
    expect(screen.getByRole('meter', {name: 'Memory reserved'})).toHaveAttribute('aria-valuenow', '81')
    expect(screen.getByText('21.4 / 46 GiB')).toBeInTheDocument()
    expect(screen.getByText('Node worker-2 is NotReady for 18 minutes')).toBeInTheDocument()
    expect(within(screen.getAllByText('ingress-nginx').map(e => e.closest('tr')).find(Boolean) as HTMLElement).getAllByRole('cell')[1]).toHaveTextContent('3')
  })

  it('shows only what is reserved, and says why, when there is no metrics-server; red from 90 %', async () => {
    const be = makeFake({hosts: [web]})
    be.state.k8s.overview = {...be.state.k8s.overview, capacity: {...be.state.k8s.overview.capacity, cpu_reserved: 11, cpu_used: null, mem_used_gib: null}}
    await openKubernetes({remember: CTX, be})
    await screen.findByRole('heading', {name: 'Capacity'})
    expect(screen.getByRole('meter', {name: 'CPU reserved'})).toHaveAttribute('aria-valuenow', '91.67')
    expect(screen.getByRole('meter', {name: 'CPU reserved'})).toHaveClass('bad')
    expect(screen.getByRole('meter', {name: 'Memory reserved'})).toHaveClass('warn') // 81 %
    expect(screen.getByRole('meter', {name: 'Pods used 44 of 330'})).toHaveClass('ok')
    expect(screen.queryByRole('meter', {name: 'CPU used'})).toBeNull()
    expect(screen.queryByRole('meter', {name: 'Memory used'})).toBeNull()
    expect(screen.getByText(/needs a metrics-server/)).toBeInTheDocument()
  })

  it('says that the amounts are those of the namespaces shown when only some are counted', async () => {
    const be = makeFake({hosts: [web]})
    be.state.k8s.overview = {...be.state.k8s.overview, capacity: {...be.state.k8s.overview.capacity, partial: true}}
    await openKubernetes({remember: CTX, be})
    await screen.findByRole('heading', {name: 'Capacity'})
    expect(screen.getByText(/those of the namespaces shown/)).toBeInTheDocument()
  })

  it('has an Open button per problem that jumps to the right view', async () => {
    await openOn('Overview')
    const open = async (text: string) => userEvent.click(within(screen.getByText(text).closest('.sp-attn') as HTMLElement).getByRole('button', {name: 'Open'}))
    await open('Node worker-2 is NotReady for 18 minutes')
    expect(await screen.findByRole('columnheader', {name: /Role/})).toBeInTheDocument() // Nodes
    expect(subTab('Nodes')).toHaveAttribute('aria-selected', 'true')
    await goTo('Overview')
    await open('Pod api-7d9f6c-x2kq8 in shop: CrashLoopBackOff, 14 restarts')
    expect(subTab('Pods')).toHaveAttribute('aria-selected', 'true')
    expect(await screen.findByRole('textbox', {name: 'Filter pods'})).toHaveValue('api-7d9f6c-x2kq8')
    await goTo('Overview')
    await open('Deployment api has 1 of 3 replicas ready')
    expect(subTab('Workloads')).toHaveAttribute('aria-selected', 'true')
    await goTo('Overview')
    await open('Certificate of Ingress shop.example.com expires in 9 days')
    expect(subTab('Network')).toHaveAttribute('aria-selected', 'true')
    await goTo('Overview')
    await open('PVC data-db-0 is 91% full')
    expect(subTab('Config & storage')).toHaveAttribute('aria-selected', 'true')
  })

  it('asks again every 5 s only while the tab is shown', async () => {
    vi.useFakeTimers({shouldAdvanceTime: true})
    const be = makeFake({hosts: [web]})
    const view = await openKubernetes({remember: CTX, be, visible: false})
    await screen.findByText('41 / 44')
    expect(calls(be, 'server.k8sOverview')).toHaveLength(1)
    await act(async () => { await vi.advanceTimersByTimeAsync(16000) })
    expect(calls(be, 'server.k8sOverview')).toHaveLength(1) // hidden: no polling
    view.unmount()
    const be2 = makeFake({hosts: [web]})
    await openKubernetes({remember: CTX, be: be2, visible: true})
    await screen.findByText('41 / 44')
    const first = calls(be2, 'server.k8sOverview').length
    await act(async () => { await vi.advanceTimersByTimeAsync(11000) })
    expect(calls(be2, 'server.k8sOverview').length).toBeGreaterThanOrEqual(first + 2)
  })

  it('does not poll the other views', async () => {
    vi.useFakeTimers({shouldAdvanceTime: true})
    const be = makeFake({hosts: [web]})
    await openKubernetes({remember: CTX, be})
    await screen.findByRole('tablist', {name: 'Kubernetes views'})
    await goTo('Nodes')
    await screen.findByText('worker-2')
    const n = calls(be, 'server.k8sNodes').length
    await act(async () => { await vi.advanceTimersByTimeAsync(16000) })
    expect(calls(be, 'server.k8sNodes')).toHaveLength(n)
  })
})

describe('Pods', () => {
  it('lists the pods, filters them, and shows only the problems on request', async () => {
    const {be} = await openOn('Pods')
    expect(await screen.findByText('redis-0')).toBeInTheDocument()
    expect(calls(be, 'server.k8sPods')[0]).toEqual(['server.k8sPods', CTX, ''])
    expect(screen.getByRole('status')).toHaveTextContent('6 of 6 pods')
    await userEvent.click(screen.getByRole('button', {name: 'Problems only'}))
    expect(screen.getByRole('button', {name: 'Problems only'})).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('status')).toHaveTextContent('3 of 6 pods')
    expect(screen.queryByText('redis-0')).toBeNull()
    expect(screen.getByText('Back-off restarting failed container (exit code 1)')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Problems only'}))
    await userEvent.type(screen.getByRole('textbox', {name: 'Filter pods'}), 'web-6b')
    expect(screen.getByRole('status')).toHaveTextContent('2 of 6 pods')
    expect(screen.queryByText('redis-0')).toBeNull()
  })

  it('sorts by a column and turns the order round on a second click (numbers start with the biggest)', async () => {
    await openOn('Pods')
    await screen.findByText('redis-0')
    // the order of the columns is the order of the whole list when there are no groups (inside each group otherwise)
    await userEvent.click(screen.getByRole('combobox', {name: /^Group by,/}))
    await userEvent.click(await screen.findByRole('option', {name: 'No groups'}))
    const names = () => screen.getAllByRole('row').slice(1).map(r => within(r).getAllByRole('cell')[0].querySelector('b')?.textContent).filter(Boolean)
    const restarts = screen.getByRole('columnheader', {name: /Restarts/})
    expect(restarts).toHaveAttribute('aria-sort', 'none')
    await userEvent.click(within(restarts).getByRole('button'))
    expect(restarts).toHaveAttribute('aria-sort', 'descending')
    expect(names()[0]).toBe('api-7d9f6c-x2kq8') // 14 restarts
    await userEvent.click(within(restarts).getByRole('button'))
    expect(restarts).toHaveAttribute('aria-sort', 'ascending')
    expect(names()[names().length - 1]).toBe('api-7d9f6c-x2kq8')
    await userEvent.click(within(screen.getByRole('columnheader', {name: /^Pod/})).getByRole('button'))
    expect(names()).toEqual([...names()].sort((a, b) => a!.localeCompare(b!)))
  })

  it('groups the pods by node as Rancher does, with the number of pods in each group, and a group can be closed', async () => {
    await openOn('Pods')
    await screen.findByText('redis-0')
    expect(screen.getByRole('combobox', {name: /^Group by,/})).toHaveTextContent('Group by node')
    const heads = screen.getAllByRole('button', {name: /^Node: /})
    expect(heads.length).toBeGreaterThan(1)
    expect(heads.map(h => h.getAttribute('aria-label'))).toEqual([...heads.map(h => h.getAttribute('aria-label'))].sort((a, b) => a!.localeCompare(b!)))
    const w1 = screen.getByRole('button', {name: 'Node: worker-1'})
    const inW1 = () => { const rows: string[] = []; for (let r = w1.closest('tbody')!.querySelectorAll('tr.k8s-group ~ tr'), i = 0; i < r.length; i++) rows.push(r[i].querySelector('b')!.textContent!); return rows }
    expect(inW1().length).toBeGreaterThan(0)
    expect(w1).toHaveTextContent(new RegExp(`${inW1().length} pods?`))
    await userEvent.click(w1)
    expect(w1).toHaveAttribute('aria-expanded', 'false')
    expect(inW1()).toEqual([]) // the pods of a closed group are not listed
    await userEvent.click(w1)
    expect(inW1().length).toBeGreaterThan(0)
  })

  it('can group by namespace or by the workload that owns the pods, or not at all, and remembers the choice', async () => {
    await openOn('Pods')
    await screen.findByText('redis-0')
    const pick = async (name: string) => { await userEvent.click(screen.getByRole('combobox', {name: /^Group by,/})); await userEvent.click(await screen.findByRole('option', {name})) }
    await pick('Group by namespace')
    expect(screen.getByRole('button', {name: 'Namespace: shop'})).toBeInTheDocument()
    await pick('Group by workload')
    expect(screen.getByRole('button', {name: 'Deployment web'})).toBeInTheDocument() // "ReplicaSet web-6b7d9 ← Deployment web"
    expect(screen.getByRole('button', {name: 'StatefulSet redis'})).toBeInTheDocument()
    expect(localStorage.getItem('vutty.k8s.pods.group')).toBe('workload')
    await pick('No groups')
    expect(screen.queryByRole('button', {name: /^Node: /})).toBeNull()
    expect(screen.queryByRole('button', {name: 'Namespace: shop'})).toBeNull()
  })

  it('keeps the filter and the sort when the list is asked again', async () => {
    vi.useFakeTimers({shouldAdvanceTime: true})
    const {be} = await openOn('Pods')
    await screen.findByText('redis-0')
    await userEvent.type(screen.getByRole('textbox', {name: 'Filter pods'}), 'web')
    await userEvent.click(within(screen.getByRole('columnheader', {name: /Restarts/})).getByRole('button'))
    const n = calls(be, 'server.k8sPods').length
    await act(async () => { await vi.advanceTimersByTimeAsync(5500) })
    expect(calls(be, 'server.k8sPods').length).toBeGreaterThan(n)
    expect(screen.getByRole('textbox', {name: 'Filter pods'})).toHaveValue('web')
    expect(screen.getByRole('columnheader', {name: /Restarts/})).toHaveAttribute('aria-sort', 'descending')
  })

  it('asks which container for a pod with several, and never offers an init container', async () => {
    const {be, onShell} = await openOn('Pods')
    await screen.findByText('web-6b7d9-m4zwn')
    await userEvent.click(within(podRow('web-6b7d9-m4zwn')).getByRole('button', {name: /^Shell/}))
    const menu = screen.getByRole('menu', {name: 'Open a shell in web-6b7d9-m4zwn'})
    expect(within(menu).getByRole('menuitem', {name: /init-config/})).toBeDisabled()
    expect(within(menu).getByRole('menuitem', {name: /nginx-sidecar/})).toBeEnabled()
    expect(calls(be, 'server.k8sShellCommand')).toHaveLength(0)
    await userEvent.click(within(menu).getByRole('menuitem', {name: /^web/}))
    await waitFor(() => expect(onShell).toHaveBeenCalled())
    expect(calls(be, 'server.k8sShellCommand')).toEqual([['server.k8sShellCommand', CTX, 'shop', 'web-6b7d9-m4zwn', 'web']])
    expect(onShell).toHaveBeenCalledWith(expect.objectContaining({id: 'h1'}), be.state.k8s.shell, 'web-1 · web-6b7d9-m4zwn')
    expect(screen.queryByRole('menu')).toBeNull()
  })

  it('opens the shell at once for a pod with one container, and is locked for a pod with none running', async () => {
    const {be, onShell} = await openOn('Pods')
    await screen.findByText('redis-0')
    await userEvent.click(within(podRow('redis-0')).getByRole('button', {name: 'Shell'}))
    await waitFor(() => expect(onShell).toHaveBeenCalledTimes(1))
    expect(calls(be, 'server.k8sShellCommand')[0]).toEqual(['server.k8sShellCommand', CTX, 'shop', 'redis-0', 'redis'])
    const bad = within(podRow('api-7d9f6c-x2kq8')).getByRole('button', {name: 'Shell'})
    expect(bad).toBeDisabled()
    expect(bad).toHaveAttribute('title', 'Needs a running container: this pod has none')
  })

  it('locks Shell, Logs and Delete with the missing right in the tooltip', async () => {
    await openOn('Pods', {rights: {exec: false, logs: false, delete_pod: false}})
    await screen.findByText('redis-0')
    const row = podRow('redis-0')
    expect(within(row).getByRole('button', {name: 'Shell'})).toBeDisabled()
    expect(within(row).getByRole('button', {name: 'Shell'})).toHaveAttribute('title', 'This login may not open a shell in pods')
    expect(within(row).getByRole('button', {name: 'Logs'})).toHaveAttribute('title', 'This login may not read logs')
    expect(within(row).getByRole('button', {name: 'Delete pod redis-0'})).toBeDisabled()
    expect(within(row).getByRole('button', {name: 'Delete pod redis-0'})).toHaveAttribute('title', 'This login may not delete pods')
  })

  it('asks before deleting a pod, naming it, its namespace and the cluster; No does nothing', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = await openOn('Pods')
    await screen.findByText('redis-0')
    await userEvent.click(screen.getByRole('button', {name: 'Delete pod redis-0'}))
    expect(ask).toHaveBeenCalledWith(expect.stringContaining('Delete the pod redis-0 in shop on prod-cluster'))
    expect(calls(be, 'server.k8sDeletePod')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Delete pod redis-0'}))
    await waitFor(() => expect(calls(be, 'server.k8sDeletePod')).toEqual([['server.k8sDeletePod', CTX, 'shop', 'redis-0']]))
    await waitFor(() => expect(screen.queryByText('redis-0')).toBeNull())
  })

  it('shows the error of a failed change in a toast and keeps the pod', async () => {
    const be = makeFake({hosts: [web]})
    be.state.k8s.fail = 'pods "redis-0" is forbidden'
    await openOn('Pods', {be})
    await screen.findByText('redis-0')
    await userEvent.click(screen.getByRole('button', {name: 'Delete pod redis-0'}))
    expect(await screen.findByText('pods "redis-0" is forbidden')).toBeInTheDocument()
    expect(screen.getByText('redis-0')).toBeInTheDocument()
    expect(screen.getByRole('button', {name: 'Delete pod redis-0'})).not.toHaveAttribute('aria-busy', 'true')
  })

  it('opens a pod with its conditions, containers and events, and reads its previous logs', async () => {
    const {be} = await openOn('Pods')
    await userEvent.click(await screen.findByRole('button', {name: 'api-7d9f6c-x2kq8'}))
    expect(await screen.findByRole('heading', {name: 'api-7d9f6c-x2kq8'})).toBeInTheDocument()
    expect(calls(be, 'server.k8sPod')[0]).toEqual(['server.k8sPod', CTX, 'shop', 'api-7d9f6c-x2kq8'])
    expect(screen.getByText('PodScheduled')).toBeInTheDocument()
    expect(screen.getByText('Waiting: CrashLoopBackOff')).toBeInTheDocument()
    expect(screen.getByText('Back-off restarting failed container api in pod api-7d9f6c-x2kq8')).toBeInTheDocument()
    expect(screen.getByText(/Environment variables are not shown here/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Previous logs of api'}))
    // the Logs tab opens on that pod, with Previous container on
    expect(await screen.findByRole('button', {name: 'Previous container'})).toHaveAttribute('aria-pressed', 'true')
    await waitFor(() => expect(calls(be, 'server.k8sLogs').pop()).toEqual(['server.k8sLogs', CTX, 'shop', 'api-7d9f6c-x2kq8', {container: 'api', lines: 300, since: '', previous: true, timestamps: false}]))
    expect(await screen.findByText(/ECONNREFUSED 10.96.0.15:5432/)).toBeInTheDocument()
    expect(screen.getByRole('button', {name: 'Timestamps'})).toHaveAttribute('aria-pressed', 'false')
  })

  it('goes back from a pod to the list', async () => {
    await openOn('Pods')
    await userEvent.click(await screen.findByRole('button', {name: 'redis-0'}))
    await screen.findByRole('heading', {name: 'redis-0'})
    await userEvent.click(screen.getByRole('button', {name: 'Pods'}))
    expect(await screen.findByRole('textbox', {name: 'Filter pods'})).toBeInTheDocument()
  })
})

describe('Logs tab with a pod source', () => {
  it('shows Timestamps only for a pod, asks again when it changes, and has no Previous container switch of its own', async () => {
    const {be} = await openOn('Pods')
    await screen.findByText('redis-0')
    await userEvent.click(within(podRow('redis-0')).getByRole('button', {name: 'Logs'}))
    await screen.findByRole('button', {name: 'Timestamps'})
    expect(screen.queryByRole('button', {name: 'Previous container'})).toBeNull() // only a "Previous logs" of a container turns it on
    await waitFor(() => expect(calls(be, 'server.k8sLogs')).toHaveLength(1))
    expect(calls(be, 'server.k8sLogs')[0][4]).toEqual({container: '', lines: 300, since: '', previous: false, timestamps: false}) // no container named: the pod's own default, as kubectl takes it
    expect(screen.getByRole('combobox', {name: /^Container,/})).toHaveTextContent('Default container')
    await userEvent.click(screen.getByRole('button', {name: 'Timestamps'}))
    await waitFor(() => expect(calls(be, 'server.k8sLogs').pop()![4]).toEqual({container: '', lines: 300, since: '', previous: false, timestamps: true}))
    expect(screen.getByRole('combobox', {name: /^Log source/})).toHaveTextContent('redis-0')
    // another kind of source: the button goes away
    await userEvent.click(screen.getByRole('combobox', {name: /^Log source/}))
    await userEvent.click(await screen.findByRole('option', {name: /Whole system/}))
    expect(screen.queryByRole('button', {name: 'Timestamps'})).toBeNull()
  })

  it('shows the colours of a program as colours (and not as boxes and "[32m"), and searches, counts and saves the plain text', async () => {
    const ESC = '\u001b'
    const be = makeFake({hosts: [web]})
    be.state.k8s.logs = [
      `${ESC}[32m[Nest]${ESC}[0m 1  - ${ESC}[2m04/11/2026, 21:41:25${ESC}[0m  ${ESC}[33m[ChatAiService]${ESC}[0m : ${ESC}[32mINFO${ESC}[0m: send chat ok duration=4809ms`,
      `${ESC}[32m[Nest]${ESC}[0m 1  - ${ESC}[31mERROR${ESC}[0m [TokenService] boom`,
      'a plain line',
    ].join('\n')
    const saved: string[] = []
    be.system.saveText = async (_t: string, _n: string, text: string) => { saved.push(text); return '/tmp/x.log' }
    await openOn('Pods', {be})
    await screen.findByText('redis-0')
    await userEvent.click(within(podRow('redis-0')).getByRole('button', {name: 'Logs'}))
    const log = await screen.findByRole('log')
    await waitFor(() => expect(log.textContent).toContain('ChatAiService'))
    expect(log.textContent).not.toMatch(/\u001b|\[32m|\[0m/) // no escape left in what is drawn
    expect(log.textContent).toContain('[Nest] 1  - 04/11/2026, 21:41:25  [ChatAiService] : INFO: send chat ok duration=4809ms')
    const green = Array.from(log.querySelectorAll('span')).filter(e => e.getAttribute('style')?.includes('color'))
    expect(green.map(e => e.textContent)).toEqual(expect.arrayContaining(['[Nest]', '[ChatAiService]', 'INFO']))
    // an error line is an error, whatever its colour codes say
    expect(screen.getByText(/TokenService/).closest('.sp-log-line')).toHaveClass('err')
    // a search finds the text, not the codes
    await userEvent.type(screen.getByRole('textbox', {name: 'Filter log lines'}), '32m')
    expect(await screen.findByText('No log lines.')).toBeInTheDocument()
    await userEvent.clear(screen.getByRole('textbox', {name: 'Filter log lines'}))
    await userEvent.type(screen.getByRole('textbox', {name: 'Filter log lines'}), 'ChatAi')
    expect(log.querySelectorAll('.sp-log-line')).toHaveLength(1)
    expect(log.querySelector('mark')?.textContent).toBe('ChatAi')
    await userEvent.clear(screen.getByRole('textbox', {name: 'Filter log lines'}))
    await userEvent.click(screen.getByRole('button', {name: /Download/}))
    await waitFor(() => expect(saved).toHaveLength(1))
    expect(saved[0]).not.toMatch(/\u001b/)
    expect(saved[0]).toContain('[ChatAiService] : INFO: send chat ok')
  })

  it('lists the pods of the cluster among the sources', async () => {
    await openOn('Pods')
    await screen.findByText('redis-0')
    await userEvent.click(screen.getByRole('tab', {name: 'Logs'}))
    await userEvent.click(await screen.findByRole('combobox', {name: /^Log source/}))
    expect(await screen.findByRole('option', {name: /web-6b7d9-q8t2c/})).toBeInTheDocument()
  })
})

describe('Workloads', () => {
  it('lists Deployments with their buttons, and switches kind', async () => {
    await openOn('Workloads')
    expect(await screen.findByText('registry/api:1.8')).toBeInTheDocument()
    expect(within(rowOf('registry/api:1.8')).getByText('1 / 3')).toHaveClass('warn')
    expect(within(screen.getByRole('tablist', {name: 'Kind of workload'})).getByRole('tab', {name: /StatefulSets/})).toHaveTextContent('2')
    await userEvent.click(screen.getByRole('tab', {name: /CronJobs/}))
    const row = within(screen.getByText('cron-sync').closest('tr') as HTMLElement)
    expect(row.getByText('*/15 * * * *')).toBeInTheDocument()
    expect(row.getByText('ImagePullBackOff')).toBeInTheDocument()
    expect(row.getByRole('button', {name: 'Run cron-sync now'})).toBeInTheDocument()
    await userEvent.click(screen.getByRole('tab', {name: /^Jobs/}))
    expect(screen.getByText('migrate-20261103')).toBeInTheDocument()
  })

  it('scales through a dialog with a stepper, sends the call, and only that button waits', async () => {
    const be = makeFake({hosts: [web]})
    let release = () => {}
    be.state.k8s.hold = new Promise<void>(r => { release = r })
    await openOn('Workloads', {be})
    await screen.findByText('registry/api:1.8')
    await userEvent.click(screen.getByRole('button', {name: 'Scale api'}))
    const dlg = screen.getByRole('dialog', {name: 'Scale api'})
    expect(within(dlg).getByText('Scale the Deployment api in shop on prod-cluster?')).toBeInTheDocument()
    expect(within(dlg).getByText(/An autoscaler \(HPA\) owns the replicas/)).toBeInTheDocument()
    expect(within(dlg).getByLabelText('Replicas')).toHaveTextContent('3')
    await userEvent.click(within(dlg).getByRole('button', {name: 'One more'}))
    await userEvent.click(within(dlg).getByRole('button', {name: 'One more'}))
    expect(within(dlg).getByLabelText('Replicas')).toHaveTextContent('5')
    await userEvent.click(within(dlg).getByRole('button', {name: 'Scale to 5'}))
    expect(screen.queryByRole('dialog')).toBeNull()
    await waitFor(() => expect(calls(be, 'server.k8sScale')).toEqual([['server.k8sScale', CTX, 'shop', 'api', 5]]))
    const mine = screen.getByRole('button', {name: 'Scale api'})
    expect(mine).toHaveAttribute('aria-busy', 'true')
    expect(mine).toBeDisabled()
    for (const other of ['Restart api', 'Scale web', 'Restart web', 'Scale worker']) {
      expect(screen.getByRole('button', {name: other})).toBeEnabled()
      expect(screen.getByRole('button', {name: other})).not.toHaveAttribute('aria-busy', 'true')
    }
    release()
    await waitFor(() => expect(screen.getByRole('button', {name: 'Scale api'})).not.toHaveAttribute('aria-busy', 'true'))
    expect(screen.getByRole('button', {name: 'Scale api'})).toBeEnabled()
  })

  it('keeps a change running (and its button locked) while another view is open', async () => {
    const be = makeFake({hosts: [web]})
    let release = () => {}
    be.state.k8s.hold = new Promise<void>(r => { release = r })
    await openOn('Workloads', {be})
    await screen.findByText('registry/api:1.8')
    await userEvent.click(screen.getByRole('button', {name: 'Restart web'}))
    await waitFor(() => expect(calls(be, 'server.k8sRestart')).toHaveLength(1))
    await goTo('Nodes')
    await goTo('Workloads')
    expect(await screen.findByRole('button', {name: 'Restart web'})).toHaveAttribute('aria-busy', 'true')
    release()
    await waitFor(() => expect(screen.getByRole('button', {name: 'Restart web'})).not.toHaveAttribute('aria-busy', 'true'))
  })

  it('does nothing when the Scale dialog is cancelled, and locks Scale without the right', async () => {
    const {be} = await openOn('Workloads')
    await screen.findByText('registry/api:1.8')
    await userEvent.click(screen.getByRole('button', {name: 'Scale api'}))
    await userEvent.click(within(screen.getByRole('dialog')).getByRole('button', {name: 'Cancel'}))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(calls(be, 'server.k8sScale')).toHaveLength(0)
  })

  it('locks Scale and Restart with a tooltip when the login may not', async () => {
    await openOn('Workloads', {rights: {scale: false, restart: false}})
    await screen.findByText('registry/api:1.8')
    expect(screen.getByRole('button', {name: 'Scale api'})).toBeDisabled()
    expect(screen.getByRole('button', {name: 'Scale api'})).toHaveAttribute('title', 'This login may not scale Deployments')
    expect(screen.getByRole('button', {name: 'Restart api'})).toHaveAttribute('title', 'This login may not restart or roll back Deployments')
  })

  it('asks before a Restart, naming the object, namespace and cluster; No does nothing', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = await openOn('Workloads')
    await screen.findByText('registry/api:1.8')
    await userEvent.click(screen.getByRole('button', {name: 'Restart api'}))
    expect(ask).toHaveBeenCalledWith(expect.stringContaining('Restart the Deployment api in shop on prod-cluster'))
    expect(calls(be, 'server.k8sRestart')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Restart api'}))
    await waitFor(() => expect(calls(be, 'server.k8sRestart')).toEqual([['server.k8sRestart', CTX, 'shop', 'deployment', 'api']]))
  })

  it('offers Restart but no Scale for StatefulSets and DaemonSets', async () => {
    const {be} = await openOn('Workloads')
    await screen.findByText('registry/api:1.8')
    await userEvent.click(screen.getByRole('tab', {name: /StatefulSets/}))
    expect(screen.queryByRole('button', {name: /^Scale/})).toBeNull()
    await userEvent.click(screen.getByRole('button', {name: 'Restart db'}))
    await waitFor(() => expect(calls(be, 'server.k8sRestart')).toEqual([['server.k8sRestart', CTX, 'data', 'statefulset', 'db']]))
    await userEvent.click(screen.getByRole('tab', {name: /DaemonSets/}))
    await userEvent.click(screen.getByRole('button', {name: 'Restart node-exporter'}))
    await waitFor(() => expect(calls(be, 'server.k8sRestart')).toHaveLength(2))
    expect(calls(be, 'server.k8sRestart')[1]).toEqual(['server.k8sRestart', CTX, 'monitoring', 'daemonset', 'node-exporter'])
  })

  it('asks before Run now, Suspend and Resume of a CronJob', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = await openOn('Workloads')
    await userEvent.click(await screen.findByRole('tab', {name: /CronJobs/}))
    await userEvent.click(screen.getByRole('button', {name: 'Run cron-sync now'}))
    expect(ask).toHaveBeenLastCalledWith(expect.stringContaining('Run the CronJob cron-sync in data on prod-cluster now'))
    expect(calls(be, 'server.k8sCronJob')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Run cron-sync now'}))
    await waitFor(() => expect(calls(be, 'server.k8sCronJob')).toEqual([['server.k8sCronJob', CTX, 'data', 'cron-sync', 'run']]))
    await userEvent.click(screen.getByRole('button', {name: 'Suspend cron-sync'}))
    await waitFor(() => expect(calls(be, 'server.k8sCronJob')[1]).toEqual(['server.k8sCronJob', CTX, 'data', 'cron-sync', 'suspend']))
    expect(await screen.findByRole('button', {name: 'Resume cron-sync'})).toBeInTheDocument()
    expect(screen.getByText('Suspended')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Resume cron-sync'}))
    await waitFor(() => expect(calls(be, 'server.k8sCronJob')[2]).toEqual(['server.k8sCronJob', CTX, 'data', 'cron-sync', 'resume']))
  })

  it('locks Run now and Suspend with the missing right', async () => {
    await openOn('Workloads', {rights: {cronjob_run: false, cronjob_patch: false}})
    await userEvent.click(await screen.findByRole('tab', {name: /CronJobs/}))
    expect(screen.getByRole('button', {name: 'Run cron-sync now'})).toHaveAttribute('title', 'This login may not run CronJobs')
    expect(screen.getByRole('button', {name: 'Suspend cron-sync'})).toHaveAttribute('title', 'This login may not suspend CronJobs')
  })

  it('asks before deleting a Job', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = await openOn('Workloads')
    await userEvent.click(await screen.findByRole('tab', {name: /^Jobs/}))
    await userEvent.click(screen.getByRole('button', {name: 'Delete job migrate-20261103'}))
    expect(ask).toHaveBeenCalledWith(expect.stringContaining('Delete the Job migrate-20261103 in shop on prod-cluster'))
    expect(calls(be, 'server.k8sDeleteJob')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Delete job migrate-20261103'}))
    await waitFor(() => expect(calls(be, 'server.k8sDeleteJob')).toEqual([['server.k8sDeleteJob', CTX, 'shop', 'migrate-20261103']]))
  })

  it('opens the merged log of every pod of the Deployment', async () => {
    const {be} = await openOn('Workloads')
    await screen.findByText('registry/api:1.8')
    await userEvent.click(screen.getByRole('button', {name: 'Logs of api'}))
    await waitFor(() => expect(calls(be, 'server.k8sLogsMulti').pop()!.slice(2, 5)).toEqual(['shop', 'deployment', 'api']))
    expect(await screen.findByText('2 pods read')).toBeInTheDocument()
    expect(await screen.findByText(/ECONNREFUSED 10.96.0.15:5432/)).toBeInTheDocument()
    expect(calls(be, 'server.k8sLogs')).toHaveLength(0) // not the log of one pod picked for it
  })
})

describe('Deployment detail', () => {
  const open = async (be?: ReturnType<typeof makeFake>) => {
    const r = await openOn('Workloads', {be})
    await userEvent.click(await screen.findByRole('button', {name: 'api'}))
    await screen.findByRole('heading', {name: 'api'})
    return r
  }

  it('shows the rollout, the spec, the autoscaler and the revisions', async () => {
    const {be} = await open()
    expect(calls(be, 'server.k8sDeployment')[0]).toEqual(['server.k8sDeployment', CTX, 'shop', 'api'])
    expect(screen.getByRole('img', {name: '1 of 3 updated and ready, 1 updated and starting, 1 old'})).toBeInTheDocument()
    expect(screen.getByText('Rolling out')).toBeInTheDocument()
    expect(screen.getByText('RollingUpdate · maxSurge 25% · maxUnavailable 25%')).toBeInTheDocument()
    expect(screen.getByText('min 2 · max 6 · now 3')).toBeInTheDocument()
    expect(screen.getByText('84% of target 70%')).toBeInTheDocument()
    expect(screen.getByText('none recorded')).toBeInTheDocument()
    expect(screen.queryByRole('button', {name: 'Roll back to 7'})).toBeNull() // the current one
  })

  it('asks before a roll back, and sends the revision', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = await open()
    await userEvent.click(screen.getByRole('button', {name: 'Roll back to 6'}))
    expect(ask).toHaveBeenCalledWith(expect.stringContaining('Roll back api in shop on prod-cluster to revision 6'))
    expect(calls(be, 'server.k8sRollback')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Roll back to 6'}))
    await waitFor(() => expect(calls(be, 'server.k8sRollback')).toEqual([['server.k8sRollback', CTX, 'shop', 'api', 6]]))
  })

  it('pauses after a yes and then offers Resume', async () => {
    const {be} = await open()
    await userEvent.click(screen.getByRole('button', {name: 'Pause'}))
    await waitFor(() => expect(calls(be, 'server.k8sRolloutPause')).toEqual([['server.k8sRolloutPause', CTX, 'shop', 'api', true]]))
    expect(await screen.findByRole('button', {name: 'Resume'})).toBeInTheDocument()
    expect(screen.getByText('Paused')).toBeInTheDocument()
  })

  it('opens the YAML, finds text in it, copies it and downloads it with system.saveText', async () => {
    const {be} = await open()
    await userEvent.click(screen.getByRole('button', {name: 'YAML'}))
    const code = await screen.findByRole('region', {name: 'YAML of Deployment api'})
    expect(calls(be, 'server.k8sYAML')).toEqual([['server.k8sYAML', CTX, 'deployment', 'shop', 'api']])
    expect(within(code).getByText('replicas:')).toBeInTheDocument()
    expect(screen.getByText('read-only · kubectl get deployment api -n shop -o yaml')).toBeInTheDocument()
    await userEvent.type(screen.getByRole('textbox', {name: 'Find in YAML'}), 'replicas')
    expect(screen.getByRole('status')).toHaveTextContent('1 lines match')
    expect(code.querySelector('mark')).toHaveTextContent('replicas')
    await userEvent.click(screen.getByRole('button', {name: /Copy/}))
    await waitFor(() => expect(calls(be, 'system.writeClipboard')).toHaveLength(1))
    await userEvent.click(screen.getByRole('button', {name: /Download/}))
    await waitFor(() => expect(calls(be, 'system.saveText')).toEqual([['system.saveText', 'deployment-api.yaml', be.state.k8s.yaml]]))
    expect(await screen.findByText('Saved to /tmp/deployment-api.yaml')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Back'}))
    expect(await screen.findByRole('heading', {name: 'api'})).toBeInTheDocument() // back to the Deployment
  })
})

describe('Network', () => {
  it('lists Services with their endpoints and Ingress with the days its certificate has left', async () => {
    await openOn('Network')
    expect(await screen.findByText('10.96.14.8')).toBeInTheDocument()
    expect(within(rowOf('10.96.14.8')).getByText('1 ready, 2 not')).toHaveClass('warn')
    expect(within(rowOf('10.96.7.2')).getByText('0 ready')).toHaveClass('bad')
    expect(within(rowOf('10.96.7.2')).queryByRole('button', {name: /Port-forward/})).toBeNull() // no endpoint: nothing to forward to
    expect(screen.getAllByText('expires in 9 days')[0]).toHaveClass('warn')
    expect(screen.getByText('valid 71 days')).toHaveClass('ok')
    expect(screen.getByText(/Address of the ingress controller: 162.43.38.137/)).toBeInTheDocument()
  })

  it('searches each group on its own, counts what is shown, and says when nothing matches', async () => {
    await openOn('Network')
    await screen.findByText('10.96.14.8')
    const services = screen.getByRole('button', {name: 'Services'}).closest('section')!
    const ingress = screen.getByRole('button', {name: 'Ingress'}).closest('section')!
    expect(within(services).getByText('4')).toBeInTheDocument()
    expect(within(ingress).getByText('3')).toBeInTheDocument()
    await userEvent.type(within(services).getByRole('textbox', {name: 'Search Services'}), 'lb')
    expect(within(services).getAllByRole('row')).toHaveLength(2) // the header and legacy-lb
    expect(within(services).getByText('1 / 4')).toBeInTheDocument()
    expect(within(ingress).getAllByRole('row')).toHaveLength(4) // the other group is not searched
    await userEvent.type(within(ingress).getByRole('textbox', {name: 'Search Ingress'}), 'admin.example')
    expect(within(ingress).getByText('1 / 3')).toBeInTheDocument()
    await userEvent.clear(within(services).getByRole('textbox', {name: 'Search Services'}))
    await userEvent.type(within(services).getByRole('textbox', {name: 'Search Services'}), 'zzz')
    expect(within(services).getByText('No Service matches.')).toBeInTheDocument()
  })

  it('closes and opens a group, keeps what was searched, and hides the box of a closed group', async () => {
    await openOn('Network')
    await screen.findByText('10.96.14.8')
    const services = () => screen.getByRole('button', {name: 'Services'}).closest('section')!
    await userEvent.type(within(services()).getByRole('textbox', {name: 'Search Services'}), 'api')
    const fold = screen.getByRole('button', {name: 'Services'})
    expect(fold).toHaveAttribute('aria-expanded', 'true')
    await userEvent.click(fold)
    expect(fold).toHaveAttribute('aria-expanded', 'false')
    expect(within(services()).queryByRole('table')).toBeNull()
    expect(within(services()).queryByRole('textbox', {name: 'Search Services'})).toBeNull()
    expect(screen.getAllByRole('table').length).toBeGreaterThan(0) // the other groups stay open
    await userEvent.click(fold)
    expect(within(services()).getByRole('textbox', {name: 'Search Services'})).toHaveValue('api')
    expect(within(services()).getByText('1 / 4')).toBeInTheDocument()
  })

  it('starts a port-forward from a dialog that shows the route, lists it, opens it and stops it', async () => {
    const {be} = await openOn('Network')
    await userEvent.click(await screen.findByRole('button', {name: 'Port-forward api'}))
    const dlg = screen.getByRole('dialog', {name: 'Port-forward api'})
    expect(within(dlg).getByText('Port-forward the Service api in shop on prod-cluster?')).toBeInTheDocument()
    expect(within(dlg).getByLabelText('Route')).toHaveTextContent('localhost:8080→ SSH →web-1 127.0.0.1:a free port→ kubectl →shop/api:80')
    const local = within(dlg).getByRole('textbox', {name: 'Port on this Mac'})
    await userEvent.clear(local)
    await userEvent.type(local, '80')
    expect(within(dlg).getByRole('button', {name: 'Start'})).toBeDisabled() // below 1024
    await userEvent.clear(local)
    await userEvent.type(local, '9090')
    await userEvent.click(within(dlg).getByRole('button', {name: 'Start'}))
    await waitFor(() => expect(calls(be, 'server.k8sPortForwardStart')).toEqual([['server.k8sPortForwardStart', CTX, 'shop', 'api', 9090, 80]]))
    expect(await screen.findByText('localhost:9090 → shop/api:80')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Open in browser'}))
    expect(calls(be, 'system.openUrl')).toEqual([['system.openUrl', 'http://localhost:9090']])
    await userEvent.click(screen.getByRole('button', {name: 'Stop port-forward 9090'}))
    await waitFor(() => expect(calls(be, 'server.k8sPortForwardStop')).toEqual([['server.k8sPortForwardStop', 'pf1']]))
    expect(await screen.findByText('No port-forward is open.')).toBeInTheDocument()
  })

  it('locks Port-forward without the right', async () => {
    await openOn('Network', {rights: {portforward: false}})
    expect(await screen.findByRole('button', {name: 'Port-forward api'})).toBeDisabled()
    expect(screen.getByRole('button', {name: 'Port-forward api'})).toHaveAttribute('title', 'This login may not forward ports')
  })
})

describe('Config and storage', () => {
  it('searches ConfigMaps, Secrets and Volumes each on its own (also by key and by who uses them), and each group can be closed', async () => {
    await openOn('Config & storage')
    await screen.findByText('api-config')
    const box = (g: string) => screen.getByRole('button', {name: g}).closest('section')!
    await userEvent.type(within(box('ConfigMaps')).getByRole('textbox', {name: 'Search ConfigMaps'}), 'default.conf') // a key
    expect(within(box('ConfigMaps')).getByText('web-nginx')).toBeInTheDocument()
    expect(within(box('ConfigMaps')).queryByText('api-config')).toBeNull()
    expect(within(box('ConfigMaps')).getByText('1 / 2')).toBeInTheDocument()
    await userEvent.type(within(box('Secrets')).getByRole('textbox', {name: 'Search Secrets'}), 'worker') // who uses it
    expect(within(box('Secrets')).getByText('db-credentials')).toBeInTheDocument()
    expect(within(box('Secrets')).queryByText('api-tls')).toBeNull()
    await userEvent.type(within(box('Volumes')).getByRole('textbox', {name: 'Search Volumes'}), 'released')
    expect(within(box('Volumes')).getByText('scratch-old')).toBeInTheDocument()
    expect(within(box('Volumes')).queryByText('data-db-0')).toBeNull()
    await userEvent.click(screen.getByRole('button', {name: 'Volumes'}))
    expect(within(box('Volumes')).queryByRole('table')).toBeNull()
    expect(within(box('Secrets')).getByRole('table')).toBeInTheDocument()
  })

  it('shows ConfigMaps (View), Secrets by name and keys, and the use of volumes', async () => {
    await openOn('Config & storage')
    expect(await screen.findByText('LOG_LEVEL, DB_HOST, FEATURE_FLAGS')).toBeInTheDocument()
    expect(screen.getByText('tls.crt, tls.key')).toBeInTheDocument()
    expect(screen.getByRole('meter', {name: 'Use of data-db-0'})).toHaveAttribute('aria-valuenow', '91')
    expect(screen.getByText('not used by any pod')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'View api-config'}))
    const dlg = await screen.findByRole('dialog', {name: 'ConfigMap api-config'})
    expect(await within(dlg).findByText('db.data.svc')).toBeInTheDocument()
    expect(within(dlg).getByText('LOG_LEVEL')).toBeInTheDocument()
  })

  it('locks Reveal when the login may not get secrets', async () => {
    await openOn('Config & storage', {rights: {secrets: false}})
    const b = await screen.findByRole('button', {name: 'Reveal a key of db-credentials'})
    expect(b).toBeDisabled()
    expect(b).toHaveAttribute('title', 'This login may not get secrets')
  })

  it('asks before Reveal, shows one key, and hides it again after 30 seconds', async () => {
    vi.useFakeTimers({shouldAdvanceTime: true})
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = await openOn('Config & storage')
    await userEvent.click(await screen.findByRole('button', {name: 'Reveal a key of db-credentials'}))
    await userEvent.click(screen.getByRole('button', {name: 'password'}))
    expect(ask).toHaveBeenCalledWith(expect.stringContaining('Reveal the key password of the Secret db-credentials in shop on prod-cluster'))
    expect(calls(be, 'server.k8sSecretKey')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Reveal a key of db-credentials'}))
    await userEvent.click(screen.getByRole('button', {name: 'password'}))
    await waitFor(() => expect(calls(be, 'server.k8sSecretKey')).toEqual([['server.k8sSecretKey', CTX, 'shop', 'db-credentials', 'password']]))
    const shown = await screen.findByRole('status', {name: 'Revealed secret'})
    expect(shown).toHaveTextContent('hunter2')
    expect(shown).not.toHaveTextContent('shop' + 'username')
    await act(async () => { await vi.advanceTimersByTimeAsync(29000) })
    expect(screen.getByRole('status', {name: 'Revealed secret'})).toBeInTheDocument()
    await act(async () => { await vi.advanceTimersByTimeAsync(1500) })
    expect(screen.queryByRole('status', {name: 'Revealed secret'})).toBeNull()
    expect(screen.queryByText('hunter2')).toBeNull()
  })

  it('can be hidden at once', async () => {
    await openOn('Config & storage')
    await userEvent.click(await screen.findByRole('button', {name: 'Reveal a key of db-credentials'}))
    await userEvent.click(screen.getByRole('button', {name: 'username'}))
    expect(await screen.findByText('shop', {selector: 'pre'})).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Hide'}))
    expect(screen.queryByRole('status', {name: 'Revealed secret'})).toBeNull()
  })
})

describe('Nodes', () => {
  it('lists nodes with their meters, and says "no data" without metrics', async () => {
    await openOn('Nodes')
    expect(await screen.findByText('10.0.0.21 · v1.30.2')).toBeInTheDocument()
    expect(screen.getByRole('meter', {name: 'Memory of worker-1'})).toHaveAttribute('aria-valuenow', '86')
    const bad = rowOf('worker-2')
    expect(within(bad).getByText('NotReady')).toHaveClass('bad')
    expect(within(bad).getByText('Unschedulable')).toBeInTheDocument()
    expect(within(bad).getAllByText('no data')).toHaveLength(2)
    expect(within(bad).getByRole('button', {name: 'Uncordon worker-2'})).toBeInTheDocument()
  })

  it('asks before Cordon, names node and cluster, and then offers Uncordon', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = await openOn('Nodes')
    await userEvent.click(await screen.findByRole('button', {name: 'Cordon worker-1'}))
    expect(ask).toHaveBeenCalledWith(expect.stringContaining('Cordon the node worker-1 on prod-cluster'))
    expect(calls(be, 'server.k8sCordon')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Cordon worker-1'}))
    await waitFor(() => expect(calls(be, 'server.k8sCordon')).toEqual([['server.k8sCordon', CTX, 'worker-1', true]]))
    expect(await screen.findByRole('button', {name: 'Uncordon worker-1'})).toBeInTheDocument()
  })

  it('locks Cordon and Drain without the right', async () => {
    await openOn('Nodes', {rights: {cordon: false, drain: false}})
    expect(await screen.findByRole('button', {name: 'Cordon worker-1'})).toHaveAttribute('title', 'This login may not cordon nodes')
    expect(screen.getByRole('button', {name: 'Drain worker-1'})).toBeDisabled()
    expect(screen.getByRole('button', {name: 'Drain worker-1'})).toHaveAttribute('title', 'This login may not drain nodes')
  })

  it('plans a drain first, blocks on what needs a say, warns about a PodDisruptionBudget, and asks before it runs', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = await openOn('Nodes')
    await userEvent.click(await screen.findByRole('button', {name: 'Drain worker-1'}))
    expect(await screen.findByRole('heading', {name: 'Drain worker-1'})).toBeInTheDocument()
    expect(await screen.findByText('14 owned by controllers')).toBeInTheDocument()
    expect(screen.getByText('batch-debug')).toBeInTheDocument()
    expect(screen.getByRole('alert')).toHaveTextContent(/PodDisruptionBudget of api in shop: allows 1 pod down/)
    expect(calls(be, 'server.k8sDrain')).toHaveLength(0)
    const drain = screen.getByRole('button', {name: 'Drain worker-1', hidden: false})
    await waitFor(() => expect(drain).toBeDisabled()) // a pod without a controller blocks
    await userEvent.click(screen.getByRole('checkbox', {name: /Also remove pods that use emptyDir/}))
    await userEvent.click(screen.getByRole('checkbox', {name: /Delete pods that have no controller/}))
    await waitFor(() => expect(calls(be, 'server.k8sDrainPlan').pop()![3]).toMatchObject({ignore_daemonsets: true, delete_emptydir: true, force: true}))
    await waitFor(() => expect(drain).toBeEnabled())
    await userEvent.click(drain)
    expect(ask).toHaveBeenCalledWith(expect.stringContaining('Drain the node worker-1 on prod-cluster'))
    expect(calls(be, 'server.k8sDrain')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(drain)
    await waitFor(() => expect(calls(be, 'server.k8sDrain')).toEqual([['server.k8sDrain', CTX, 'worker-1', {ignore_daemonsets: true, delete_emptydir: true, force: true, grace_sec: 30, timeout_sec: 300}]]))
    expect(await screen.findByRole('button', {name: 'Uncordon worker-1'})).toBeInTheDocument() // back on the list
  })

  it('can cordon only, after asking, from the drain page', async () => {
    const ask = vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = await openOn('Nodes')
    await userEvent.click(await screen.findByRole('button', {name: 'Drain worker-1'}))
    await screen.findByText('14 owned by controllers')
    await userEvent.click(screen.getByRole('button', {name: 'Cordon only'}))
    expect(ask).toHaveBeenCalledWith(expect.stringContaining('Cordon the node worker-1'))
    expect(calls(be, 'server.k8sCordon')).toHaveLength(0)
    ask.mockReturnValue(true)
    await userEvent.click(screen.getByRole('button', {name: 'Cordon only'}))
    await waitFor(() => expect(calls(be, 'server.k8sCordon')).toEqual([['server.k8sCordon', CTX, 'worker-1', true]]))
  })

  it('goes back from the drain page without doing anything', async () => {
    const {be} = await openOn('Nodes')
    await userEvent.click(await screen.findByRole('button', {name: 'Drain worker-1'}))
    await screen.findByText('14 owned by controllers')
    await userEvent.click(screen.getByRole('button', {name: 'Cancel'}))
    expect(await screen.findByRole('button', {name: 'Cordon worker-1'})).toBeInTheDocument()
    expect(calls(be, 'server.k8sDrain')).toHaveLength(0)
  })
})

describe('Events', () => {
  it('shows the warnings of the last hour, newest first, and Normal on request', async () => {
    await openOn('Events')
    expect(await screen.findByText('BackOff')).toBeInTheDocument()
    const reasons = () => screen.getAllByRole('row').slice(1).map(r => within(r).getAllByRole('cell')[1].textContent)
    expect(reasons()).toEqual(['BackOff', 'FailedScheduling', 'Failed', 'NodeNotReady', 'Unhealthy'])
    expect(screen.queryByText('OldNews')).toBeNull() // older than an hour
    expect(screen.queryByText('Pulled')).toBeNull() // Normal is off
    await userEvent.click(screen.getByRole('button', {name: 'Normal'}))
    expect(screen.getByText('Pulled')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'Warnings'}))
    expect(reasons()).toEqual(['Pulled'])
    await userEvent.click(screen.getByRole('button', {name: 'Warnings'}))
    await userEvent.type(screen.getByRole('textbox', {name: 'Filter events'}), 'worker-2')
    expect(reasons()).toEqual(['NodeNotReady'])
  })
})

describe('Namespace', () => {
  it('starts with all namespaces and asks the cluster for one when chosen, also for the rights', async () => {
    const {be} = await openOn('Pods')
    await screen.findByText('redis-0')
    expect(screen.getByRole('combobox', {name: /^Namespace/})).toHaveTextContent('All namespaces')
    await userEvent.click(screen.getByRole('combobox', {name: /^Namespace/}))
    await userEvent.click(await screen.findByRole('option', {name: 'data'}))
    await waitFor(() => expect(calls(be, 'server.k8sPods').pop()).toEqual(['server.k8sPods', CTX, 'data']))
    expect(calls(be, 'server.k8sAccess').pop()).toEqual(['server.k8sAccess', CTX, 'data'])
    expect(await screen.findByText('cron-sync-28811-6lvdm')).toBeInTheDocument()
    expect(screen.queryByText('redis-0')).toBeNull()
  })
})

describe('What a screen keeps when the namespace is changed', () => {
  async function inTwoNamespaces() {
    const be = makeFake({hosts: [web]})
    be.server.k8sNamespaces = async () => ({names: ['shop', 'data']})
    await openOn('Pods', {be})
    await screen.findByText('redis-0')
    return be
  }
  const changeNs = async (name: string) => {
    await userEvent.click(screen.getByRole('combobox', {name: /^Namespace,/}))
    await userEvent.click(await screen.findByRole('option', {name}))
  }

  it('keeps the filter and the switch of Pods', async () => {
    await inTwoNamespaces()
    await userEvent.type(screen.getByRole('textbox', {name: 'Filter pods'}), 're')
    await userEvent.click(screen.getByRole('button', {name: 'Problems only'}))
    await changeNs('shop')
    expect(screen.getByRole('textbox', {name: 'Filter pods'})).toHaveValue('re')
    expect(screen.getByRole('button', {name: 'Problems only'})).toHaveAttribute('aria-pressed', 'true')
  })

  it('keeps the groups of Pods that were closed', async () => {
    await inTwoNamespaces()
    const head = screen.getAllByRole('button', {name: /^Node: /})[0]
    const label = head.getAttribute('aria-label')!
    await userEvent.click(head)
    expect(screen.getByRole('button', {name: label})).toHaveAttribute('aria-expanded', 'false')
    await changeNs('shop')
    expect(await screen.findByRole('button', {name: label})).toHaveAttribute('aria-expanded', 'false')
  })

  it('keeps the search and the closed boxes of Network and of Config & storage, and the kind of workload', async () => {
    await inTwoNamespaces()
    await goTo('Network')
    await userEvent.type(await screen.findByRole('textbox', {name: 'Search Services'}), 'lb')
    await userEvent.click(screen.getByRole('button', {name: 'Ingress'})) // closed
    await goTo('Config & storage')
    await userEvent.type(await screen.findByRole('textbox', {name: 'Search ConfigMaps'}), 'web')
    await goTo('Workloads')
    await userEvent.click(await screen.findByRole('tab', {name: /StatefulSets/}))
    await userEvent.type(screen.getByRole('textbox', {name: 'Filter workloads'}), 'db')
    await changeNs('shop')
    expect(screen.getByRole('tab', {name: /StatefulSets/})).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('textbox', {name: 'Filter workloads'})).toHaveValue('db')
    await goTo('Network')
    expect(await screen.findByRole('textbox', {name: 'Search Services'})).toHaveValue('lb')
    expect(screen.getByRole('button', {name: 'Ingress'})).toHaveAttribute('aria-expanded', 'false')
    await goTo('Config & storage')
    expect(await screen.findByRole('textbox', {name: 'Search ConfigMaps'})).toHaveValue('web')
  })
})

describe('Loading', () => {
  /** a screen whose first answer has not come: the call is held until the test lets it go */
  function held(call: 'k8sOverview' | 'k8sPods' | 'k8sNetwork' | 'k8sConfig') {
    const be = makeFake({hosts: [web]})
    const real = be.server[call] as (...a: unknown[]) => Promise<unknown>
    let go: () => void = () => {}
    const gate = new Promise<void>(r => { go = r })
    ;(be.server as unknown as Record<string, unknown>)[call] = async (...a: unknown[]) => { await gate; return real(...a) }
    return {be, go}
  }

  it('shows a ring and what is being loaded in the middle of the screen, until the answer comes', async () => {
    const {be, go} = held('k8sOverview')
    await openKubernetes({remember: CTX, be})
    const wait = await screen.findByRole('status')
    expect(wait).toHaveClass('ld')
    expect(wait).toHaveAttribute('aria-busy', 'true')
    expect(within(wait).getByText('Loading the overview…')).toBeInTheDocument()
    expect(screen.queryByRole('heading', {name: 'Capacity'})).toBeNull()
    go()
    expect(await screen.findByRole('heading', {name: 'Capacity'})).toBeInTheDocument()
    expect(screen.queryByRole('status', {busy: true})).toBeNull()
  })

  it('says which list is being loaded, and the list replaces it', async () => {
    const {be, go} = held('k8sPods')
    await openOn('Pods', {be})
    expect(await within(await screen.findByRole('status')).findByText('Loading pods…')).toBeInTheDocument()
    expect(screen.queryByRole('textbox', {name: 'Filter pods'})).toBeNull()
    go()
    expect(await screen.findByText('redis-0')).toBeInTheDocument()
    expect(screen.queryByRole('status', {busy: true})).toBeNull()
  })

  it('shows nothing of the kind when the call fails: the error is said', async () => {
    const be = makeFake({hosts: [web]})
    be.server.k8sNetwork = async () => { throw new Error('services is forbidden: cannot list') }
    await openOn('Network', {be})
    expect(await screen.findByText(/services is forbidden/)).toBeInTheDocument()
    expect(screen.queryByRole('status', {busy: true})).toBeNull()
  })
})

describe('Back', () => {
  const backBtn = () => screen.getByRole('button', {name: 'Back to the previous screen'})
  it('goes back to the view before, by the button, the back button of a mouse and Alt+Left', async () => {
    await openOn('Overview')
    expect(backBtn()).toBeDisabled()
    await goTo('Pods')
    await goTo('Events')
    expect(subTab('Events')).toHaveAttribute('aria-selected', 'true')
    await userEvent.click(backBtn())
    expect(subTab('Pods')).toHaveAttribute('aria-selected', 'true')
    await goTo('Events')
    act(() => { window.dispatchEvent(new MouseEvent('mouseup', {button: 3, bubbles: true, cancelable: true})) })
    expect(subTab('Pods')).toHaveAttribute('aria-selected', 'true')
    await userEvent.keyboard('{Alt>}{ArrowLeft}{/Alt}')
    expect(subTab('Overview')).toHaveAttribute('aria-selected', 'true')
    expect(backBtn()).toBeDisabled()
  })
})
