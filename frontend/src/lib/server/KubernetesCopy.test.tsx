import {screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {calls, openOn} from '../../test/k8sKit'
import {kubectlLines, shq} from './KubernetesCopy'

const t = (s: string) => s

describe('the kubectl lines', () => {
  it('quote what is not plain and carry the context and the namespace', () => {
    expect(shq('prod')).toBe('prod')
    expect(shq("a b'c")).toBe("'a b'\\''c'")
    const l = kubectlLines(t, 'my ctx', {kind: 'pod', ns: 'shop', name: 'web-1', container: 'app'})
    expect(l.find(x => x.label === 'Open a shell')!.cmd).toBe("kubectl exec -it web-1 -c app -n shop --context 'my ctx' -- sh")
    expect(l.find(x => x.label === 'Forward a port')!.cmd).toBe("kubectl port-forward pod/web-1 8080:80 -n shop --context 'my ctx'")
    const n = kubectlLines(t, 'c', {kind: 'node', name: 'w1', ip: '10.0.0.5', jump: 'ubuntu@w1.example.com'})
    expect(n.find(x => x.label === 'SSH to this node')!.cmd).toBe('ssh -J ubuntu@w1.example.com 10.0.0.5')
    expect(n.every(x => !x.cmd.includes(' -n '))).toBe(true) // a node has no namespace
    expect(kubectlLines(t, 'c', {kind: 'node', name: 'w1'}).some(x => x.label === 'SSH to this node')).toBe(false)
    expect(kubectlLines(t, 'c', {kind: 'deployment', ns: 'shop', name: 'web'}).map(x => x.label)).toContain('Rollout status')
  })
})

describe('Copy kubectl', () => {
  it('copies a line from a node row, and nothing else happens', async () => {
    const {be} = await openOn('Nodes')
    await screen.findByRole('table')
    await userEvent.click(screen.getAllByRole('button', {name: 'Copy kubectl ▾'})[0])
    const menu = await screen.findByRole('menu')
    await userEvent.click(within(menu).getByRole('menuitem', {name: /Describe/}))
    await waitFor(() => expect(calls(be, 'system.writeClipboard').pop()![1]).toMatch(/^kubectl describe node \S+ --context prod-cluster$/))
    expect(screen.queryByRole('menu')).not.toBeInTheDocument()
  })
})
