import {render, screen, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {ServerPanel} from '../lib/server/ServerPanel'
import {host, makeFake, Providers} from './fakeBackend'

export type Fake = ReturnType<typeof makeFake>
export const web = host({id: 'h1', label: 'web-1', hostname: 'w1.example.com', username: 'ubuntu', port: 22})
export const calls = (be: Fake, n: string) => be.state.calls.filter(c => c[0] === n)

/** the Panel of web-1 with the Kubernetes tab open; with `remember` the cluster is the one chosen last time, so the tab goes straight in */
export async function openKubernetes(o: {remember?: string; visible?: boolean; be?: Fake} = {}) {
  localStorage.clear()
  if (o.remember) localStorage.setItem('vutty.k8s.context.h1', o.remember)
  const be = o.be ?? makeFake({hosts: [web]})
  const onShell = vi.fn()
  const view = render(<Providers be={be}><ServerPanel host={web} visible={o.visible ?? true} onShell={onShell} /></Providers>)
  await screen.findByText(/up 41d 0h/)
  await userEvent.click(screen.getByRole('tab', {name: 'Kubernetes'}))
  return {be, onShell, ...view}
}

/** like openKubernetes with the cluster remembered, but keeps what the test put in localStorage (a saved list of namespaces) */
export async function openKubernetesKeepingStorage(be: Fake) {
  const onShell = vi.fn()
  const view = render(<Providers be={be}><ServerPanel host={web} visible onShell={onShell} /></Providers>)
  await screen.findByText(/up 41d 0h/)
  await userEvent.click(screen.getByRole('tab', {name: 'Kubernetes'}))
  await screen.findByRole('tablist', {name: 'Kubernetes views'})
  return {be, onShell, ...view}
}

/** the choice of a cluster, then "Open Kubernetes" on the rights page */
export async function connect(name = 'prod-cluster') {
  await userEvent.click(await screen.findByRole('radio', {name: new RegExp(name)}))
  await userEvent.click(screen.getByRole('button', {name: 'Continue'}))
  await userEvent.click(await screen.findByRole('button', {name: 'Open Kubernetes'}))
}

export const subTab = (name: string) => within(screen.getByRole('tablist', {name: 'Kubernetes views'})).getByRole('tab', {name})
export const goTo = async (name: string) => { await userEvent.click(subTab(name)) }
export const rowOf = (text: string) => screen.getByText(text).closest('tr') as HTMLElement

/** a cluster that is already open on the given view */
export async function openOn(view: string, o: {be?: Fake; rights?: Record<string, boolean>} = {}) {
  const be = o.be ?? makeFake({hosts: [web]})
  if (o.rights) be.state.k8s.rights = o.rights
  const r = await openKubernetes({remember: 'prod-cluster', be})
  await screen.findByRole('tablist', {name: 'Kubernetes views'})
  if (view !== 'Overview') await goTo(view)
  return r
}
