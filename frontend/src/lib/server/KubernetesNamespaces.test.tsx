import {screen, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {calls, openOn, web} from '../../test/k8sKit'
import {makeFake} from '../../test/fakeBackend'

const FORBIDDEN = 'namespaces is forbidden: User "u-1" cannot list resource "namespaces" in API group "" at the cluster scope'

/** a login that may only look inside its own namespace: the list of namespaces and the cluster-wide lists are refused */
function restricted() {
  const be = makeFake({hosts: [web]})
  const s = be.server
  s.k8sNamespaces = async () => { throw new Error(FORBIDDEN) }
  const pods = s.k8sPods
  s.k8sPods = async (sid, c, n) => { if (!n) throw new Error('pods is forbidden: cannot list at the cluster scope'); return pods(sid, c, n) }
  const overview = s.k8sOverview
  s.k8sOverview = async (sid, c, n) => { if (!n) throw new Error('pods is forbidden: cannot list at the cluster scope'); return {...(await overview(sid, c, n)), nodes_hidden: true, cpu_requested_pct: null, mem_requested_pct: null, capacity: {...(await overview(sid, c, n)).capacity, known: false}} }
  return be
}

const addNamespace = async (name: string) => {
  const open = screen.queryByRole('button', {name: 'Add namespace'})
  if (open) await userEvent.click(open) // after a refusal the box is still open
  await userEvent.type(screen.getByRole('textbox', {name: 'Namespace name'}), name + '{Enter}')
}

describe('a login that may not list the namespaces', () => {
  it('says so, instead of an error, and lets a namespace be added by name', async () => {
    const be = restricted()
    await openOn('Pods', {be})
    expect(await screen.findByText(/may not list the namespaces of the cluster, so there is no list to choose from/)).toBeInTheDocument()
    expect(screen.queryByText(FORBIDDEN)).not.toBeInTheDocument()
    await addNamespace('shop')
    expect(await screen.findByText('api-7d9f6c-x2kq8')).toBeInTheDocument() // the pods of that namespace are asked for
    expect(calls(be, 'server.k8sPods').some(c => c[2] === 'shop')).toBe(true)
    expect(screen.getByRole('combobox', {name: /^Namespace,/})).toHaveTextContent('shop')
  })

  it('refuses a name that is not a namespace name, and sends nothing', async () => {
    const be = restricted()
    await openOn('Pods', {be})
    await screen.findByText(/no list to choose from/)
    for (const bad of ['Shop', 'a b', '-x', 'x_y', 'a'.repeat(64)]) {
      await addNamespace(bad)
      expect((await screen.findAllByRole('alert')).some(a => /That is not a namespace name/.test(a.textContent ?? ''))).toBe(true)
      expect(screen.getByRole('textbox', {name: 'Namespace name'})).toBeInTheDocument() // still asking
      await userEvent.clear(screen.getByRole('textbox', {name: 'Namespace name'}))
    }
    expect(calls(be, 'server.k8sPods').some(c => c[2] && c[2] !== '')).toBe(false)
  })

  it('remembers the namespaces for this cluster and starts in all of them', async () => {
    const be = restricted()
    const first = await openOn('Pods', {be})
    await screen.findByText(/no list to choose from/)
    await addNamespace('data')
    await addNamespace('shop')
    expect(JSON.parse(localStorage.getItem('vutty.k8s.namespaces.h1|prod-cluster') ?? '[]')).toEqual(['shop', 'data'])
    first.unmount()
    // a new tab of the same cluster: no list again, but it starts in the last one used and offers both
    const be2 = restricted()
    // the kit clears localStorage when it opens, so the saved list is put back first
    localStorage.setItem('vutty.k8s.namespaces.h1|prod-cluster', JSON.stringify(['shop', 'data']))
    const kit = await import('../../test/k8sKit')
    localStorage.setItem('vutty.k8s.context.h1', 'prod-cluster')
    const ui = await kit.openKubernetesKeepingStorage(be2)
    await kit.goTo('Pods')
    expect(await screen.findByRole('combobox', {name: /^Namespace,/})).toHaveTextContent('All added namespaces (2)')
    expect(calls(ui.be, 'server.k8sPods').some(c => c[2] === 'shop,data')).toBe(true) // asked together, one namespace after the other by the backend
  })

  it('offers all the namespaces that were added as one choice, and one by one', async () => {
    const be = restricted()
    await openOn('Pods', {be})
    await screen.findByText(/no list to choose from/)
    await addNamespace('shop')
    expect(screen.getByRole('combobox', {name: /^Namespace,/})).toHaveTextContent('shop')
    await addNamespace('data') // the second one shows both
    await userEvent.click(screen.getByRole('combobox', {name: /^Namespace,/}))
    expect(await screen.findByRole('option', {name: 'All added namespaces (2)'})).toBeInTheDocument()
    expect(screen.getByRole('option', {name: 'data'})).toBeInTheDocument()
    expect(screen.queryByRole('option', {name: 'All namespaces'})).not.toBeInTheDocument() // that one needs the cluster-wide right
    await userEvent.click(screen.getByRole('option', {name: 'data'}))
    expect(calls(be, 'server.k8sPods').some(c => c[2] === 'data')).toBe(true)
    expect(screen.queryByRole('button', {name: 'Forget namespace shop'})).not.toBeInTheDocument()
    expect(screen.getByRole('button', {name: 'Forget namespace data'})).toBeInTheDocument()
  })

  it('can forget a namespace that was added', async () => {
    const be = restricted()
    await openOn('Pods', {be})
    await screen.findByText(/no list to choose from/)
    await addNamespace('shop')
    await userEvent.click(await screen.findByRole('button', {name: 'Forget namespace shop'}))
    expect(JSON.parse(localStorage.getItem('vutty.k8s.namespaces.h1|prod-cluster') ?? '[]')).toEqual([])
    expect(screen.queryByRole('button', {name: 'Forget namespace shop'})).not.toBeInTheDocument()
  })

  it('shows the Overview of a namespace, with the nodes marked as not readable', async () => {
    const be = restricted()
    await openOn('Overview', {be})
    await screen.findByText(/no list to choose from/)
    await addNamespace('shop')
    expect(await screen.findByText('This login may not read the nodes of the cluster.')).toBeInTheDocument()
    expect(calls(be, 'server.k8sOverview').some(c => c[2] === 'shop')).toBe(true)
    expect(within(document.body).queryAllByText('Needs the nodes: this login may not read them.').length).toBeGreaterThan(0)
  })

  it('keeps the plain list and no hint for a login that may list them', async () => {
    const be = makeFake({hosts: [web]})
    await openOn('Pods', {be})
    await userEvent.click(await screen.findByRole('combobox', {name: /^Namespace,/}))
    expect(await screen.findByRole('option', {name: 'shop'})).toBeInTheDocument()
    expect(screen.queryByText(/no list to choose from/)).not.toBeInTheDocument()
  })
})
