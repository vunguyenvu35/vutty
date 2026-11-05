import {screen, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {calls, openOn} from '../../test/k8sKit'

const pick = async (kind: string) => {
  await userEvent.click(await screen.findByRole('combobox', {name: /^Kind/}))
  await userEvent.click(await screen.findByRole('option', {name: new RegExp('^' + kind)}))
}

describe('Resources: any kind of the cluster, read only', () => {
  it('asks for a kind, shows the columns it asks for and opens an object as YAML', async () => {
    const {be} = await openOn('Resources')
    expect(await screen.findByText(/Choose a kind to see its objects/)).toBeInTheDocument()
    expect(screen.getByText('3 kinds · read only')).toBeInTheDocument()
    await pick('Certificate')
    const table = await screen.findByRole('table')
    expect(within(table).getAllByRole('columnheader').map(h => h.textContent?.replace(/[▲▼ ]/g, ''))).toEqual(['Name', 'Namespace', 'Ready', 'Secret', 'Age', ''])
    expect(within(table).getByText('web-tls')).toBeInTheDocument()
    expect(calls(be, 'server.k8sObjects')[0].slice(1, 3)).toEqual(['prod-cluster', ''])
    expect(calls(be, 'server.k8sObjects')[0][3]).toEqual({group: 'cert-manager.io', version: 'v1', plural: 'certificates'})

    await userEvent.type(screen.getByRole('textbox', {name: 'Filter objects'}), 'db-tls')
    expect(within(table).queryByText('web-tls')).not.toBeInTheDocument()
    expect(within(table).getByText('db-tls')).toBeInTheDocument()
    await userEvent.clear(screen.getByRole('textbox', {name: 'Filter objects'}))

    await userEvent.click(screen.getByRole('button', {name: 'YAML of web'}))
    expect(await screen.findByRole('region', {name: 'YAML of Certificate web'})).toHaveTextContent('secretName: web-tls')
    expect(screen.getByText('read-only · kubectl get certificates.cert-manager.io web -n shop -o yaml')).toBeInTheDocument()
    expect(calls(be, 'server.k8sObjectYAML')[0].slice(1, 3)).toEqual(['prod-cluster', 'shop'])
    expect(calls(be, 'server.k8sYAML')).toHaveLength(0)
    await userEvent.click(screen.getByRole('button', {name: 'Back'})) // back to the list, the kind still chosen
    expect(await screen.findByText('web-tls')).toBeInTheDocument()
  })

  it('has no Namespace column for a kind that is cluster-wide, and says when there are more objects', async () => {
    const {be} = await openOn('Resources')
    be.state.k8s.objects.nodes.more = true
    await pick('Node')
    const table = await screen.findByRole('table')
    expect(within(table).queryByRole('columnheader', {name: /Namespace/})).not.toBeInTheDocument()
    expect(within(table).getByText('worker-1')).toBeInTheDocument()
    expect(await screen.findByText(/more objects than are shown/)).toBeInTheDocument()
  })
})
