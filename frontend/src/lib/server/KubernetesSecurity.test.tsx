import {screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {calls, openOn} from '../../test/k8sKit'

describe('Security: what the cluster exposes, read only', () => {
  it('lists the findings, high first, hides the low ones to start with and filters', async () => {
    const {be} = await openOn('Security')
    const table = await screen.findByRole('table')
    expect(calls(be, 'server.k8sSecurity')[0].slice(1)).toEqual(['prod-cluster', ''])
    expect(screen.getByText('3 pods and 2 services looked at · read only')).toBeInTheDocument()
    const rows = within(table).getAllByRole('row').slice(1)
    expect(rows).toHaveLength(2) // the low one is switched off
    expect(within(rows[0]).getByText('High')).toHaveClass('bad')
    expect(rows[0]).toHaveTextContent('Privileged container')
    expect(rows[0]).toHaveTextContent('Deployment · shop · app')
    expect(rows[0]).toHaveTextContent('(2 pods)')
    expect(rows[1]).toHaveTextContent('Secret in plain text')
    expect(table).not.toHaveTextContent('hunter2')

    await userEvent.click(screen.getByRole('button', {name: 'Low · 1'}))
    expect(within(table).getByText('Image not pinned')).toBeInTheDocument()
    await userEvent.type(screen.getByRole('textbox', {name: 'Filter findings'}), 'DB_PASSWORD')
    expect(within(table).getAllByRole('row')).toHaveLength(2)
    await userEvent.clear(screen.getByRole('textbox', {name: 'Filter findings'}))
    await userEvent.type(screen.getByRole('textbox', {name: 'Filter findings'}), 'zzz')
    expect(await screen.findByText('No finding matches.')).toBeInTheDocument()
  })

  it('opens the Deployment of a finding, and says so when there is nothing to report', async () => {
    const {be} = await openOn('Security')
    await userEvent.click(await screen.findByRole('button', {name: 'web'}))
    await waitFor(() => expect(calls(be, 'server.k8sDeployment').pop()!.slice(1)).toEqual(['prod-cluster', 'shop', 'web']))
  })

  it('has an empty cluster say nothing to report', async () => {
    const {be} = await openOn('Overview')
    be.state.k8s.security = {findings: [], pods: 1, services: 1}
    await userEvent.click(within(screen.getByRole('tablist', {name: 'Kubernetes views'})).getByRole('tab', {name: 'Security'}))
    await waitFor(() => expect(screen.getByText('Nothing to report.')).toBeInTheDocument())
  })
})
