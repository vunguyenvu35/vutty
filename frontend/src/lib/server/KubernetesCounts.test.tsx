import {screen, waitFor} from '@testing-library/react'
import {calls, openOn} from '../../test/k8sKit'

describe('how many there are, on the tabs', () => {
  it('shows the number of pods, workloads and network objects, and asks again for the namespace chosen', async () => {
    const {be} = await openOn('Overview')
    const k = be.state.k8s
    const pods = screen.getByRole('tab', {name: 'Pods'}) // the number is for the eye: the name of the tab stays the same
    await waitFor(() => expect(pods).toHaveTextContent(new RegExp(`Pods${k.pods.length}$`)))
    expect(screen.getByRole('tab', {name: 'Workloads'})).toHaveTextContent(new RegExp(`Workloads${k.workloads.length}$`))
    expect(screen.getByRole('tab', {name: 'Network'})).toHaveTextContent(/Network\d+$/)
    expect(screen.getByRole('tab', {name: 'Events'})).toHaveTextContent(/^Events$/) // what that tab shows is not a count of a kind
    expect(calls(be, 'server.k8sCounts')[0].slice(1)).toEqual(['prod-cluster', ''])
  })
})
