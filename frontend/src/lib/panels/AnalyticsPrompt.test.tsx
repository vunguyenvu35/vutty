import {describe, expect, it} from 'vitest'
import {render, screen, waitFor} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {AnalyticsPrompt} from './AnalyticsPrompt'
import {makeFake, Providers} from '../../test/fakeBackend'

function setup(be: ReturnType<typeof makeFake>) {
  const seen: unknown[] = []
  render(<Providers be={be}><AnalyticsPrompt onSettings={s => seen.push(s)} /></Providers>)
  return seen
}

describe('AnalyticsPrompt', () => {
  it('stays away in a build without a Google Analytics stream', async () => {
    const be = makeFake({analytics: false})
    setup(be)
    await new Promise(r => setTimeout(r, 30))
    expect(screen.queryByRole('dialog')).toBeNull()
  })

  it('stays away once the user has answered', async () => {
    for (const answer of ['yes', 'no'] as const) {
      const be = makeFake({analytics: true, settings: {...makeFake().state.settings, analytics_consent: answer}})
      const {unmount} = render(<Providers be={be}><AnalyticsPrompt onSettings={() => {}} /></Providers>)
      await new Promise(r => setTimeout(r, 30))
      expect(screen.queryByRole('dialog')).toBeNull()
      unmount()
    }
  })

  it('asks, lists exactly what is sent, and saves Share as yes', async () => {
    const be = makeFake({analytics: true})
    const seen = setup(be)
    const dlg = await screen.findByRole('dialog', {name: 'Share anonymous usage statistics?'})
    expect(dlg).toHaveTextContent('a random id made for this installation')
    expect(dlg).toHaveTextContent('the Vutty version, the operating system and the processor type')
    expect(dlg).toHaveTextContent('that the app was opened')
    expect(dlg).toHaveTextContent('no host, key, command, file or password')
    await userEvent.click(screen.getByRole('button', {name: 'Share'}))
    await waitFor(() => expect(be.state.settings.analytics_consent).toBe('yes'))
    expect(screen.queryByRole('dialog')).toBeNull()
    expect(seen).toHaveLength(1)
  })

  it('saves No thanks as no, and so does closing the dialog', async () => {
    let be = makeFake({analytics: true})
    const {unmount} = render(<Providers be={be}><AnalyticsPrompt onSettings={() => {}} /></Providers>)
    await userEvent.click(await screen.findByRole('button', {name: 'No thanks'}))
    await waitFor(() => expect(be.state.settings.analytics_consent).toBe('no'))
    unmount()

    be = makeFake({analytics: true})
    render(<Providers be={be}><AnalyticsPrompt onSettings={() => {}} /></Providers>)
    await screen.findByRole('dialog')
    await userEvent.keyboard('{Escape}')
    await waitFor(() => expect(be.state.settings.analytics_consent).toBe('no'))
  })
})
