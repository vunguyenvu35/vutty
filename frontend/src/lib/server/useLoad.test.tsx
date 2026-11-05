import {act, render, screen} from '@testing-library/react'
import {useLoad} from './shared'

function Probe({load}: {load: () => Promise<string>}) {
  const {data} = useLoad<string>(load, 5000)
  return <p>{data ?? 'loading'}</p>
}

describe('useLoad with a timer', () => {
  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => { vi.useRealTimers() })

  it('shows an answer that takes longer than the interval, instead of asking again before it arrives', async () => {
    let calls = 0
    const load = () => { calls++; return new Promise<string>(res => setTimeout(() => res('answer'), 7000)) }
    render(<Probe load={load} />)
    await act(async () => { await vi.advanceTimersByTimeAsync(8000) })
    expect(screen.getByText('answer')).toBeInTheDocument()
    expect(calls).toBe(1) // no second request while the first is still on its way
    await act(async () => { await vi.advanceTimersByTimeAsync(5000) })
    expect(calls).toBe(2) // and the timer goes on afterwards
  })
})
