import {screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {calls, openOn, rowOf} from '../../test/k8sKit'

describe('the log of all the pods of a Service or workload', () => {
  it('opens from a Service, with the pods read, the ones left out and the ones that failed said, and each line with its pod', async () => {
    const {be} = await openOn('Network')
    const asked = be.server.k8sLogsMulti // the fake records the call, then what it answers is changed
    be.server.k8sLogsMulti = async (...a) => ({...(await asked(...a)), text: '[api-1] listening on :3000\n[api-2] Error: ECONNREFUSED\n[api-2/proxy] ready', skipped: 3, failed: ['api-9: container is waiting to start']})
    await screen.findByText('10.96.14.8')
    await userEvent.click(within(rowOf('10.96.14.8')).getByRole('button', {name: 'Logs of api'}))
    const log = await screen.findByRole('log', {name: 'Logs'})
    expect(await within(log).findByText('[api-1]')).toBeInTheDocument() // the pod name has its own colour, the line follows
    expect(within(log).getByText(/listening on :3000/)).toBeInTheDocument()
    expect(within(log).getByText('[api-2/proxy]')).toBeInTheDocument()
    expect(screen.getByText(/2 pods read · 3 more pods are not shown · Could not read: api-9: container is waiting to start/)).toBeInTheDocument()
    expect(within(log).getByText('[api-1]').getAttribute('style')).toMatch(/color: /)
    expect(calls(be, 'server.k8sLogsMulti').pop()!.slice(2, 5)).toEqual(['shop', 'service', 'api'])
    expect(screen.getByRole('combobox', {name: /^Log source/})).toHaveTextContent('api (all pods)')
    // the filter box finds a pod by its name
    await userEvent.type(screen.getByRole('textbox', {name: 'Filter log lines'}), 'api-2')
    await waitFor(() => expect(within(log).queryByText(/listening on :3000/)).not.toBeInTheDocument())
  })
})
