import {act, cleanup, fireEvent, render, screen, waitFor, within} from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import {useState} from 'react'
import {popupSize, SettingsWindow} from './SettingsWindow'
import {Switch} from '../ui/Switch'
import {makeFake, Providers} from '../../test/fakeBackend'
import type {Settings, VaultStatus} from '../types'
import {SHOW_CRYPTO_DONATE} from '../donate'
import {setLanguageSetting} from '../i18n/store'
import {stylesheet} from '../../test/stylesheet'

beforeEach(() => { try { localStorage.removeItem('vutty.settingsSize') } catch { /* none */ } })

const vaultSt: VaultStatus = {exists: true, unlocked: true, keychain_enabled: false, auto_lock_minutes: 15}

function Harness({be, vault = vaultSt, onVault = vi.fn(), onClose}: {be: ReturnType<typeof makeFake>; vault?: VaultStatus; onVault?: () => void; onClose: () => void}) {
  const [s, setS] = useState<Settings>(be.state.settings)
  return <SettingsWindow settings={s} appearance="dark" vault={vault} onSettings={setS} onVault={onVault} onChanged={vi.fn()} onClose={onClose} />
}

function setup(over: Partial<Settings> = {}, vault = vaultSt) {
  const be = makeFake({settings: {...makeFake().state.settings, ...over}})
  const onVault = vi.fn(), onClose = vi.fn()
  render(<Providers be={be}><Harness be={be} vault={vault} onVault={onVault} onClose={onClose} /></Providers>)
  return {be, onVault, onClose}
}
const tab = (n: string) => userEvent.click(screen.getByRole('tab', {name: n}))

describe('SettingsWindow: language', () => {
  afterEach(() => { act(() => setLanguageSetting('auto')) })
  it('offers Automatic and the four languages by their own names, and saves the choice', async () => {
    const {be} = setup()
    const sel = await screen.findByRole('combobox', {name: /^Language, Automatic/})
    await userEvent.click(sel)
    expect(screen.getAllByRole('option').map(o => o.textContent)).toEqual(['Automatic', 'English', 'Tiếng Việt', '日本語', '中文'])
    await userEvent.click(screen.getByRole('option', {name: '日本語'}))
    await waitFor(() => expect(be.state.settings.language).toBe('ja'))
  })
})

describe('SettingsWindow: session logs', () => {
  it('is off to begin with, with no folder shown; turning it on shows the default folder and saves the choice', async () => {
    const {be} = setup()
    await tab('Terminal')
    const sel = await screen.findByRole('combobox', {name: /^Session logs, Off/})
    expect(screen.queryByTestId('session-log-folder')).toBeNull()
    await userEvent.click(sel)
    await userEvent.click(screen.getByRole('option', {name: /Production hosts only/}))
    await waitFor(() => expect(be.state.settings.session_log).toBe('prod'))
    expect(await screen.findByTestId('session-log-folder')).toHaveTextContent('/session-logs')
    await userEvent.click(screen.getByRole('combobox', {name: /^Session logs/}))
    await userEvent.click(screen.getByRole('option', {name: /All sessions/}))
    await waitFor(() => expect(be.state.settings.session_log).toBe('all'))
  })

  it('warns what a log holds, and what is not recorded', async () => {
    setup()
    await tab('Terminal')
    expect(await screen.findByText(/a password in a file you displayed included/)).toBeInTheDocument()
    expect(screen.getByText(/What you type is not recorded/)).toBeInTheDocument()
  })

  it('chooses another folder, goes back to the default one, and opens the folder', async () => {
    const {be} = setup({session_log: 'all'})
    await tab('Terminal')
    expect(await screen.findByTestId('session-log-folder')).toHaveTextContent('/Users/me/Library/Application Support/vutty/session-logs')
    expect(screen.queryByRole('button', {name: 'Use the default'})).toBeNull()
    await userEvent.click(screen.getByRole('button', {name: 'Choose…'}))
    await waitFor(() => expect(be.state.settings.session_log_dir).toBe('/tmp/dir')) // what the fake folder picker answers
    await waitFor(() => expect(screen.getByTestId('session-log-folder')).toHaveTextContent('/tmp/dir'))
    await userEvent.click(screen.getByRole('button', {name: 'Open'}))
    await waitFor(() => expect(be.state.calls).toContainEqual(['system.openPath', '/tmp/dir']))
    await userEvent.click(screen.getByRole('button', {name: 'Use the default'}))
    await waitFor(() => expect(be.state.settings.session_log_dir).toBe(''))
    await waitFor(() => expect(screen.getByTestId('session-log-folder')).toHaveTextContent('/session-logs'))
  })
})

describe('SettingsWindow: production hosts', () => {
  it('has the switch for being careful on production hosts, on to begin with, and saves it', async () => {
    const {be} = setup()
    const sw = await screen.findByRole('switch', {name: 'Be careful on production hosts'})
    expect(sw).toBeChecked()
    await userEvent.click(sw)
    await waitFor(() => expect(be.state.settings.confirm_prod).toBe(false))
  })
})

describe('SettingsWindow', () => {
  it('is a window with a title bar, not a page', () => {
    setup()
    const win = screen.getByRole('dialog', {name: 'Settings'})
    expect(within(win).getByText('Settings', {selector: '.win-name'})).toBeInTheDocument()
    expect(document.querySelector('.win-back')).not.toBeNull()
  })

  it('has categories on the left and theme, help links and version below them', async () => {
    setup()
    const nav = screen.getByRole('navigation', {name: 'Settings menu'})
    expect(within(nav).getAllByRole('tab').map(t => t.querySelector('span')?.textContent)).toEqual(['General', 'Account', 'Terminal', 'Security', 'AI assistant', 'Sync', 'Shortcuts', 'Storage'])
    expect(within(nav).getByRole('combobox', {name: /^Theme,/})).toBeInTheDocument()
    for (const l of ['Documentation', 'Request a feature', 'Report a bug', 'Release notes', 'Support Vutty']) {
      expect(within(nav).getByRole('button', {name: new RegExp(l)})).toBeInTheDocument()
    }
    expect(await screen.findByTestId('app-version')).toHaveTextContent('1.1.0-test')
  })

  it('closes with the ×, with Escape and with a click on the backdrop (not on the panel)', async () => {
    const {onClose} = setup()
    await userEvent.click(screen.getByRole('button', {name: 'Close settings'}))
    expect(onClose).toHaveBeenCalledTimes(1)
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(2)
    fireEvent.mouseDown(screen.getByRole('dialog', {name: 'Settings'}))
    expect(onClose).toHaveBeenCalledTimes(2) // inside the panel
    fireEvent.mouseDown(document.querySelector('.win-back') as HTMLElement)
    expect(onClose).toHaveBeenCalledTimes(3)
  })

  it('Escape closes an open dropdown first and not the window', async () => {
    const {onClose} = setup()
    await userEvent.click(screen.getByRole('combobox', {name: /^Theme,/}))
    await userEvent.keyboard('{Escape}')
    expect(onClose).not.toHaveBeenCalled()
    await userEvent.keyboard('{Escape}')
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('changes the app theme to Auto, Light or Dark from the sidebar dropdown and saves immediately', async () => {
    const {be} = setup()
    for (const mode of ['auto', 'light', 'dark']) {
      await userEvent.selectOptions(screen.getByLabelText('Theme'), mode)
      await waitFor(() => expect(be.state.settings.theme).toBe(mode))
    }
  })

  it('the theme dropdown opens the custom menu', async () => {
    const {be} = setup()
    await userEvent.click(screen.getByRole('combobox', {name: /^Theme,/}))
    await userEvent.click(screen.getByRole('option', {name: 'Light'}))
    await waitFor(() => expect(be.state.settings.theme).toBe('light'))
  })

  it('picks a terminal theme, filters by dark/light, and can go back to "match app"', async () => {
    const {be} = setup()
    await tab('Terminal')
    const group = screen.getByRole('radiogroup', {name: 'Terminal theme'})
    expect(within(group).getByRole('radio', {name: /Match app theme/})).toHaveAttribute('aria-checked', 'true')
    await userEvent.click(within(group).getByRole('radio', {name: 'Dracula'}))
    await waitFor(() => expect(be.state.settings.terminal_theme).toBe('dracula'))
    expect(within(group).getByRole('radio', {name: 'Dracula'})).toHaveAttribute('aria-checked', 'true')
    await userEvent.click(screen.getByRole('button', {name: 'Light'}))
    expect(within(group).queryByRole('radio', {name: 'Dracula'})).toBeNull()
    expect(within(group).getByRole('radio', {name: 'Solarized Light'})).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', {name: 'All'}))
    await userEvent.click(within(group).getByRole('radio', {name: /Match app theme/}))
    await waitFor(() => expect(be.state.settings.terminal_theme).toBe('auto'))
  })

  it('"Match app theme" always shows the automatic choice, not the selected theme', async () => {
    setup({terminal_theme: 'nord'})
    await tab('Terminal')
    const match = screen.getByRole('radio', {name: /Match app theme/})
    expect(match).toHaveTextContent('Vutty Dark') // appearance=dark
    expect(match).not.toHaveTextContent('Nord')
  })

  it('the preview uses the chosen theme colours, font and size', async () => {
    setup({terminal_theme: 'nord', font_size: 18})
    await tab('Terminal')
    expect(screen.getByTestId('term-preview')).toHaveStyle({background: '#2e3440', fontSize: '18px'})
  })

  it('font (custom dropdown), size stepper, cursor, blinking and scrollback all persist', async () => {
    const {be} = setup()
    await tab('Terminal')
    await userEvent.click(screen.getByRole('combobox', {name: /^Font family,/}))
    await userEvent.click(screen.getByRole('option', {name: 'JetBrains Mono'}))
    await waitFor(() => expect(be.state.settings.font_family).toContain('JetBrains Mono'))
    await userEvent.click(screen.getByLabelText('Increase font size'))
    await waitFor(() => expect(be.state.settings.font_size).toBe(14))
    await userEvent.click(screen.getByLabelText('Decrease font size'))
    await waitFor(() => expect(be.state.settings.font_size).toBe(13))
    await userEvent.click(screen.getByRole('radio', {name: 'bar'}))
    await waitFor(() => expect(be.state.settings.cursor_style).toBe('bar'))
    await userEvent.click(screen.getByRole('switch', {name: 'Blinking cursor'}))
    await waitFor(() => expect(be.state.settings.cursor_blink).toBe(false))
    const sb = screen.getByLabelText('Scrollback lines')
    await userEvent.clear(sb)
    await userEvent.type(sb, '2500{Enter}')
    await waitFor(() => expect(be.state.settings.scrollback).toBe(2500))
  })

  it('stepper stops at the limits', async () => {
    setup({font_size: 8})
    await tab('Terminal')
    expect(screen.getByLabelText('Decrease font size')).toBeDisabled()
  })

  it('shows a backend validation error as a toast and keeps the old value', async () => {
    const {be} = setup()
    await tab('Terminal')
    be.settings.save = async () => { throw 'scrollback must be between 500 and 100000 lines' }
    const sb = screen.getByLabelText('Scrollback lines')
    await userEvent.clear(sb)
    await userEvent.type(sb, '5{Enter}')
    expect(await screen.findByRole('status')).toHaveTextContent('scrollback must be')
    expect(be.state.settings.scrollback).toBe(10000)
  })

  it('security: "ask on open" shows only with Touch ID turned on, and can be switched off', async () => {
    setup()
    await tab('Security')
    expect(screen.queryByRole('switch', {name: 'Ask on open'})).toBeNull()
  })

  it('security: with Touch ID on, "ask on open" is a switch that is saved', async () => {
    const {be} = setup({biometric_on_open: true}, {...vaultSt, biometric_available: true, keychain_enabled: true})
    await tab('Security')
    await userEvent.click(screen.getByRole('switch', {name: 'Ask on open'}))
    await waitFor(() => expect(be.state.settings.biometric_on_open).toBe(false))
  })

  it('Ctrl+1…8 jump to a category and Ctrl+Tab goes round them, with the keys shown on the tabs', async () => {
    setup()
    const key = (init: KeyboardEventInit) => act(() => { window.dispatchEvent(new KeyboardEvent('keydown', {bubbles: true, cancelable: true, ...init})) })
    const on = () => screen.getByRole('tab', {selected: true}).textContent
    expect(screen.getByRole('tab', {name: /^Security/})).toHaveTextContent('Ctrl+4')
    key({key: '4', code: 'Digit4', ctrlKey: true})
    expect(on()).toMatch(/^Security/)
    key({key: '8', code: 'Digit8', ctrlKey: true})
    expect(on()).toMatch(/^Storage/)
    key({key: 'Tab', code: 'Tab', ctrlKey: true}) // after the last one comes the first
    expect(on()).toMatch(/^General/)
    key({key: 'Tab', code: 'Tab', ctrlKey: true, shiftKey: true}) // and back
    expect(on()).toMatch(/^Storage/)
  })

  it('security: a password hint can be saved', async () => {
    const {be, onVault} = setup()
    await tab('Security')
    const save = screen.getAllByRole('button', {name: 'Save'}).at(-1)!
    expect(save).toBeDisabled()
    await userEvent.type(screen.getByLabelText('Password hint'), 'three random words')
    await userEvent.click(save)
    await waitFor(() => expect(be.state.vault.hint).toBe('three random words'))
    expect(onVault).toHaveBeenCalled()
  })

  it('security: locking when the computer sleeps is on to begin with, explains itself and saves the choice', async () => {
    const {be} = setup()
    await tab('Security')
    const sw = screen.getByRole('switch', {name: 'Lock when the computer sleeps'})
    expect(sw).toBeChecked()
    expect(screen.getByText(/Open terminals stay connected/)).toBeInTheDocument()
    await userEvent.click(sw)
    await waitFor(() => expect(be.state.settings.lock_on_sleep).toBe(false))
    await userEvent.click(screen.getByRole('switch', {name: 'Lock when the computer sleeps'}))
    await waitFor(() => expect(be.state.settings.lock_on_sleep).toBe(true))
  })

  it('security: auto-lock, keychain switch and password change', async () => {
    const {be, onVault} = setup()
    await tab('Security')
    const al = screen.getByLabelText('Auto-lock minutes')
    await userEvent.clear(al)
    await userEvent.type(al, '5')
    await userEvent.click(screen.getByRole('button', {name: 'Apply'}))
    await waitFor(() => expect(be.state.calls).toContainEqual(['vault.setAutoLock', 5]))
    await userEvent.click(screen.getByRole('switch', {name: 'Keychain unlock'}))
    await waitFor(() => expect(be.state.vault.keychain_enabled).toBe(true))
    expect(onVault).toHaveBeenCalled()

    const btn = screen.getByRole('button', {name: 'Change password'})
    await userEvent.type(screen.getByLabelText('Current password'), 'wrong one')
    await userEvent.type(screen.getByLabelText('New password'), 'short')
    expect(btn).toBeDisabled()
    await userEvent.clear(screen.getByLabelText('New password'))
    await userEvent.type(screen.getByLabelText('New password'), 'a brand new password')
    await userEvent.click(btn)
    expect(await screen.findByRole('alert')).toHaveTextContent('Incorrect master password. Please check it and try again.')
    await userEvent.clear(screen.getByLabelText('Current password'))
    await userEvent.type(screen.getByLabelText('Current password'), be.state.password)
    await userEvent.click(btn)
    await waitFor(() => expect(be.state.password).toBe('a brand new password'))
  })

  it('sync is shown inside the window', async () => {
    const {be} = setup()
    await tab('Sync')
    expect(await screen.findByLabelText('Repository URL')).toHaveValue('git@example.com:me/sync.git')
    await userEvent.click(screen.getByRole('button', {name: 'Sync now'}))
    await waitFor(() => expect(be.state.calls.some(c => c[0] === 'sync.syncNow')).toBe(true))
  })

  it('help links open only the project pages', async () => {
    const {be} = setup()
    await userEvent.click(screen.getByRole('button', {name: /Report a bug/}))
    expect(be.state.calls).toContainEqual(['system.openUrl', 'https://github.com/vunguyenvu35/vutty/issues/new'])
  })

  it('Support Vutty opens Ko-fi, and hides the USDT addresses while crypto donations are off', async () => {
    const {be} = setup()
    await userEvent.click(screen.getByRole('button', {name: 'Support Vutty'}))
    const dlg = await screen.findByRole('dialog', {name: 'Support Vutty'})
    if (!SHOW_CRYPTO_DONATE) expect(within(dlg).queryByText(/USDT|Prefer crypto/)).toBeNull()
    await userEvent.click(within(dlg).getByRole('button', {name: 'Support on Ko-fi'}))
    expect(be.state.calls).toContainEqual(['system.openUrl', 'https://ko-fi.com/vunguyenvu'])
  })

  it.skipIf(!SHOW_CRYPTO_DONATE)('Support Vutty opens Ko-fi and the USDT addresses (QR code, Copy)', async () => {
    const {be} = setup()
    await userEvent.click(screen.getByRole('button', {name: 'Support Vutty'}))
    const dlg = await screen.findByRole('dialog', {name: 'Support Vutty'})
    expect(within(dlg).getByText('TMXXHRNG7qauoFWJKYbqEbd4FGgt3DgjBK')).toBeInTheDocument()
    expect(within(dlg).getByText('0x4bdfd1d6456c28d06e3f8456ddac59e6168e5153')).toBeInTheDocument()
    expect(within(dlg).getAllByRole('img')).toHaveLength(2)
    await userEvent.click(within(dlg).getByRole('button', {name: 'Support on Ko-fi'}))
    expect(be.state.calls).toContainEqual(['system.openUrl', 'https://ko-fi.com/vunguyenvu'])
    await userEvent.click(within(dlg).getByRole('button', {name: 'Copy USDT · Tron address'}))
    await waitFor(() => expect(be.state.calls).toContainEqual(['system.writeClipboard', 'TMXXHRNG7qauoFWJKYbqEbd4FGgt3DgjBK']))
    await userEvent.keyboard('{Escape}') // closes the dialog, not the whole Settings window
    expect(screen.queryByRole('dialog', {name: 'Support Vutty'})).toBeNull()
    expect(screen.getByRole('navigation', {name: 'Settings menu'})).toBeInTheDocument()
  })
})

describe('SettingsWindow: the popup', () => {
  const win = () => screen.getByRole('dialog', {name: 'Settings'})

  it('has a title bar with the gear and the name at the left, Maximize and Close at the right, and no window dots', () => {
    setup()
    const bar = document.querySelector('.win-title') as HTMLElement
    expect(within(bar).getByText('Settings', {selector: '.win-name'}).querySelector('svg')).not.toBeNull()
    expect(within(bar).getAllByRole('button').map(b => b.getAttribute('aria-label'))).toEqual(['Maximize settings', 'Close settings'])
    expect(document.querySelector('.win-dots')).toBeNull()
  })

  it('maximise fills the screen, restore goes back, and the title bar double-click does the same', async () => {
    setup()
    expect(win()).not.toHaveClass('max')
    await userEvent.click(screen.getByRole('button', {name: 'Maximize settings'}))
    expect(win()).toHaveClass('max')
    expect(win().closest('.win-back')).toHaveClass('maximized')
    await userEvent.click(screen.getByRole('button', {name: 'Restore settings'}))
    expect(win()).not.toHaveClass('max')
    fireEvent.doubleClick(document.querySelector('.win-title') as HTMLElement)
    expect(win()).toHaveClass('max')
    fireEvent.doubleClick(document.querySelector('.win-title') as HTMLElement)
    expect(win()).not.toHaveClass('max')
  })

  it('is a bordered panel inset from the edges of the app (a backdrop shows around it) that fills the window when maximised', () => {
    const css = stylesheet()
    expect(css.match(/\n\.win-back \{([^}]*)\}/)![1]).toMatch(/padding:\s*56px 52px 44px/)
    expect(css.match(/\n\.win \{([^}]*)\}/)![1]).toMatch(/border-radius:\s*10px/)
    expect(css.match(/\n\.win-back\.maximized \{([^}]*)\}/)![1]).toMatch(/padding:\s*0/)
  })
})

describe('SettingsWindow: size', () => {
  const win = () => screen.getByRole('dialog', {name: 'Settings'})
  const grip = (g: string) => document.querySelector(`[data-grip="${g}"]`) as HTMLElement
  const setViewport = (w: number, h: number) => {
    Object.defineProperty(window, 'innerWidth', {configurable: true, value: w})
    Object.defineProperty(window, 'innerHeight', {configurable: true, value: h})
  }
  const original = {w: window.innerWidth, h: window.innerHeight}
  afterEach(() => setViewport(original.w, original.h))
  /** the popup is 800 x 600 for the pointer maths (jsdom has no layout) */
  const sized = () => {
    vi.spyOn(win(), 'getBoundingClientRect').mockReturnValue({width: 800, height: 600, x: 0, y: 0, top: 0, left: 0, right: 800, bottom: 600, toJSON: () => ({})} as DOMRect)
  }
  const drag = (g: string, from: [number, number], to: [number, number]) => {
    fireEvent.pointerDown(grip(g), {clientX: from[0], clientY: from[1], pointerId: 1})
    fireEvent.pointerMove(grip(g), {clientX: to[0], clientY: to[1], pointerId: 1})
    fireEvent.pointerUp(grip(g), {pointerId: 1})
  }

  it('the size comes from the window: nearly all of it, with an inset, and never more than a panel on a large screen', () => {
    expect(popupSize({w: 1000, h: 700}, null)).toEqual({w: 896, h: 600})
    expect(popupSize({w: 2560, h: 1440}, null)).toEqual({w: 1280, h: 880})
    expect(popupSize({w: 700, h: 450}, null)).toEqual({w: 640, h: 420}) // never below the minimum
    expect(popupSize({w: 1000, h: 800}, {fw: 0.5, fh: 0.5})).toEqual({w: 640, h: 420}) // a remembered share, held to the minimum
    expect(popupSize({w: 1000, h: 800}, {fw: 0.9, fh: 0.8})).toEqual({w: 900, h: 640})
  })

  it('opens at the size of the current window, and follows the window when it is resized', () => {
    setViewport(1200, 800)
    setup()
    expect(win().style.width).toBe('1096px')
    expect(win().style.height).toBe('700px')
    act(() => { setViewport(2400, 1500); window.dispatchEvent(new Event('resize')) })
    expect(win().style.width).toBe('1280px')
    expect(win().style.height).toBe('880px')
  })

  it('has a handle on every edge and corner that resizes around the centre, so the edge follows the pointer', () => {
    setViewport(1200, 800)
    setup()
    sized()
    drag('e', [500, 300], [550, 300])
    expect(win().style.width).toBe('900px')
    expect(win().style.height).toBe('600px') // an east handle leaves the height as it was (the mocked 600)
    drag('w', [100, 300], [60, 300]) // dragging a west edge to the left makes it wider
    expect(win().style.width).toBe('880px')
    drag('s', [400, 500], [400, 450]) // up: smaller
    expect(win().style.height).toBe('500px')
    drag('nw', [10, 10], [-20, -20]) // a corner changes both
    expect(win().style.width).toBe('860px')
    expect(win().style.height).toBe('660px')
  })

  it('moves with its title bar, held inside the window and below the top bar, and not by its buttons', () => {
    setViewport(1200, 800)
    setup()
    vi.spyOn(win(), 'getBoundingClientRect').mockReturnValue({width: 800, height: 600, x: 100, y: 60, top: 60, left: 100, right: 900, bottom: 660, toJSON: () => ({})} as DOMRect)
    const bar = document.querySelector('.win-title') as HTMLElement
    fireEvent.pointerDown(bar, {clientX: 300, clientY: 80, pointerId: 1})
    fireEvent.pointerMove(bar, {clientX: 340, clientY: 100, pointerId: 1})
    expect(win().style.transform).toBe('translate(40px, 20px)')
    expect(bar).toHaveClass('dragging')
    fireEvent.pointerMove(bar, {clientX: -4000, clientY: -4000, pointerId: 1}) // as far as it can go: the left edge of the window, and just under the 46px top bar
    expect(win().style.transform).toBe('translate(-100px, -14px)')
    fireEvent.pointerMove(bar, {clientX: 9000, clientY: 9000, pointerId: 1}) // and the right and bottom edges of the window
    expect(win().style.transform).toBe('translate(300px, 140px)')
    fireEvent.pointerUp(bar, {pointerId: 1})
    expect(bar).not.toHaveClass('dragging')
    fireEvent.pointerMove(bar, {clientX: 0, clientY: 0, pointerId: 1})
    expect(win().style.transform).toBe('translate(300px, 140px)') // released: no more movement
    fireEvent.pointerDown(screen.getByRole('button', {name: 'Close settings'}), {clientX: 5, clientY: 5, pointerId: 1})
    fireEvent.pointerMove(bar, {clientX: 500, clientY: 500, pointerId: 1})
    expect(win().style.transform).toBe('translate(300px, 140px)') // a button is not a handle
  })

  it('goes back to the centre when maximised and restored, and when the window changes size', async () => {
    setViewport(1200, 800)
    setup()
    vi.spyOn(win(), 'getBoundingClientRect').mockReturnValue({width: 800, height: 600, x: 100, y: 60, top: 60, left: 100, right: 900, bottom: 660, toJSON: () => ({})} as DOMRect)
    const bar = document.querySelector('.win-title') as HTMLElement
    fireEvent.pointerDown(bar, {clientX: 300, clientY: 80, pointerId: 1})
    fireEvent.pointerMove(bar, {clientX: 340, clientY: 100, pointerId: 1})
    fireEvent.pointerUp(bar, {pointerId: 1})
    await userEvent.click(screen.getByRole('button', {name: 'Maximize settings'}))
    await userEvent.click(screen.getByRole('button', {name: 'Restore settings'}))
    expect(win().style.transform).toBe('')
    vi.spyOn(win(), 'getBoundingClientRect').mockReturnValue({width: 800, height: 600, x: 100, y: 60, top: 60, left: 100, right: 900, bottom: 660, toJSON: () => ({})} as DOMRect)
    fireEvent.pointerDown(bar, {clientX: 300, clientY: 80, pointerId: 1})
    fireEvent.pointerMove(bar, {clientX: 340, clientY: 100, pointerId: 1})
    fireEvent.pointerUp(bar, {pointerId: 1})
    act(() => { window.dispatchEvent(new Event('resize')) })
    expect(win().style.transform).toBe('')
  })

  it('opens below the top bar, so it never covers the macOS window buttons', () => {
    const css = stylesheet()
    expect(css.match(/\n\.win-back \{([^}]*)\}/)![1]).toMatch(/padding:\s*56px 52px 44px/) // the top bar is 46px high
    expect(css.match(/\n\.topbar \{([^}]*)\}/)![1]).toMatch(/height:\s*46px/)
  })

  it('never goes below the minimum or beyond the window', () => {
    setViewport(1200, 800)
    setup()
    sized()
    drag('se', [500, 300], [-5000, -5000])
    expect(win().style.width).toBe('640px')
    expect(win().style.height).toBe('420px')
    drag('se', [500, 300], [9000, 9000])
    expect(win().style.width).toBe('1200px')
    expect(win().style.height).toBe('800px')
  })

  it('remembers the size as a share of the window, so another window gets a fitting one; a double click on a handle goes back to the default', () => {
    setViewport(1200, 800)
    const first = setup()
    sized()
    drag('e', [500, 300], [550, 300]) // 900 of 1200
    expect(JSON.parse(localStorage.getItem('vutty.settingsSize')!)).toEqual({fw: 0.75, fh: 600 / 800})
    cleanup()
    setViewport(1000, 700)
    setup()
    expect(win().style.width).toBe('750px') // the same share of a smaller window
    expect(first).toBeDefined()
    fireEvent.doubleClick(grip('e'))
    expect(win().style.width).toBe('896px')
    expect(localStorage.getItem('vutty.settingsSize')).toBeNull()
  })

  it('has no handles, and no fixed size, when maximised or shown as a tab', async () => {
    setViewport(1200, 800)
    setup()
    expect(document.querySelectorAll('.win-grip')).toHaveLength(8)
    await userEvent.click(screen.getByRole('button', {name: 'Maximize settings'}))
    expect(document.querySelectorAll('.win-grip')).toHaveLength(0)
    expect(win().style.width).toBe('')
    cleanup()
    render(<Providers be={makeFake()}><SettingsWindow page settings={makeFake().state.settings} appearance="dark" vault={vaultSt} onSettings={vi.fn()} onVault={vi.fn()} onChanged={vi.fn()} onClose={vi.fn()} /></Providers>)
    expect(document.querySelectorAll('.win-grip')).toHaveLength(0)
    expect(screen.getByRole('region', {name: 'Settings'}).style.width).toBe('')
  })
})

describe('SettingsWindow: open in a tab', () => {
  it('the popup has the Open in a tab button before Maximize only when it can be moved', () => {
    const {unmount} = render(<Providers be={makeFake()}><SettingsWindow settings={makeFake().state.settings} appearance="dark" vault={vaultSt} onSettings={vi.fn()} onVault={vi.fn()} onChanged={vi.fn()} onClose={vi.fn()} /></Providers>)
    expect(screen.queryByRole('button', {name: 'Open settings in a tab'})).toBeNull()
    unmount()
    const onOpenAsTab = vi.fn()
    render(<Providers be={makeFake()}><SettingsWindow settings={makeFake().state.settings} appearance="dark" vault={vaultSt} onSettings={vi.fn()} onVault={vi.fn()} onChanged={vi.fn()} onClose={vi.fn()} onOpenAsTab={onOpenAsTab} /></Providers>)
    const bar = document.querySelector('.win-title') as HTMLElement
    expect(within(bar).getAllByRole('button').map(b => b.getAttribute('aria-label'))).toEqual(['Open settings in a tab', 'Maximize settings', 'Close settings'])
    fireEvent.click(screen.getByRole('button', {name: 'Open settings in a tab'}))
    expect(onOpenAsTab).toHaveBeenCalledTimes(1)
  })

  describe('as the body of a tab (page)', () => {
    function setupPage() {
      const be = makeFake()
      const onClose = vi.fn()
      render(<Providers be={be}><SettingsWindow page settings={be.state.settings} appearance="dark" vault={vaultSt} onSettings={vi.fn()} onVault={vi.fn()} onChanged={vi.fn()} onClose={onClose} /></Providers>)
      return {be, onClose}
    }

    it('is a region without the popup\'s backdrop, title bar or category keys', () => {
      const {onClose} = setupPage()
      const region = screen.getByRole('region', {name: 'Settings'})
      expect(region).toHaveClass('page')
      expect(screen.queryByRole('dialog', {name: 'Settings'})).toBeNull()
      expect(document.querySelector('.win-title')).toBeNull()
      expect(document.querySelector('.settings-key')).toBeNull()
      fireEvent.mouseDown(document.querySelector('.win-back') as HTMLElement)
      expect(onClose).not.toHaveBeenCalled() // there is no backdrop to click
    })

    it('Esc, Mod+2 and Ctrl+Tab are left to the app', () => {
      const {onClose} = setupPage()
      fireEvent.keyDown(window, {key: 'Escape'})
      fireEvent.keyDown(window, {key: '2', ctrlKey: true, metaKey: true})
      fireEvent.keyDown(window, {key: 'Tab', ctrlKey: true})
      expect(onClose).not.toHaveBeenCalled()
      expect(screen.getByRole('tab', {name: 'General', selected: true})).toBeInTheDocument()
    })
  })
})

describe('SettingsWindow: General', () => {
  it('opens on General with zoom, reduce motion and confirm-before-close', () => {
    setup()
    expect(screen.getByRole('tab', {name: 'General', selected: true})).toBeInTheDocument()
    expect(screen.getByLabelText('Zoom level')).toHaveTextContent('100%')
    expect(screen.getByRole('switch', {name: 'Confirm before closing a tab'})).toHaveAttribute('aria-checked', 'false') // closing a tab does not ask unless you want it to
    expect(screen.getByRole('switch', {name: 'Reduce motion'})).toHaveAttribute('aria-checked', 'false')
  })

  it('zoom steps by 10%, stops at 80% and 160%, and resets', async () => {
    const {be} = setup({zoom: 100})
    await userEvent.click(screen.getByRole('button', {name: 'Zoom in'}))
    await waitFor(() => expect(be.state.settings.zoom).toBe(110))
    await userEvent.click(screen.getByRole('button', {name: 'Reset zoom'}))
    await waitFor(() => expect(be.state.settings.zoom).toBe(100))
    expect(screen.getByRole('button', {name: 'Reset zoom'})).toBeDisabled()
    for (let i = 0; i < 2; i++) await userEvent.click(screen.getByRole('button', {name: 'Zoom out'}))
    await waitFor(() => expect(be.state.settings.zoom).toBe(80))
    expect(screen.getByRole('button', {name: 'Zoom out'})).toBeDisabled()
  })

  it('zoom in is disabled at 160%', () => {
    setup({zoom: 160})
    expect(screen.getByRole('button', {name: 'Zoom in'})).toBeDisabled()
  })

  it('the switches persist', async () => {
    const {be} = setup()
    await userEvent.click(screen.getByRole('switch', {name: 'Reduce motion'}))
    await waitFor(() => expect(be.state.settings.reduce_motion).toBe(true))
    await userEvent.click(screen.getByRole('switch', {name: 'Confirm before closing a tab'}))
    await waitFor(() => expect(be.state.settings.confirm_tab_close).toBe(true))
  })
})

describe('SettingsWindow: Account', () => {
  it('shows a local profile, the free plan and the not-yet features', async () => {
    setup()
    await tab('Account')
    expect(screen.getByLabelText('Display name')).toHaveValue('')
    expect(screen.getByText('free · open source')).toBeInTheDocument()
    expect(screen.getByRole('button', {name: 'Sign in'})).toBeDisabled()
    expect(screen.getByText('coming later')).toBeInTheDocument()
    expect(screen.getByText(/stored only on this Mac/i)).toBeInTheDocument()
  })

  it('saves the profile only when it changed, and shows the initial as the avatar', async () => {
    const {be} = setup({account_name: 'Vu Nguyen', account_email: 'vu@example.com'})
    await tab('Account')
    const save = screen.getByRole('button', {name: 'Save profile'})
    expect(save).toBeDisabled()
    const name = screen.getByLabelText('Display name')
    await userEvent.clear(name)
    await userEvent.type(name, 'Ada')
    expect(save).toBeEnabled()
    await userEvent.click(save)
    await waitFor(() => expect(be.state.settings.account_name).toBe('Ada'))
    expect(document.querySelector('.avatar')).toHaveTextContent('A')
  })

  it('reports an invalid email from the backend', async () => {
    const {be} = setup()
    be.settings.save = async () => { throw 'enter a valid email address' }
    await tab('Account')
    await userEvent.type(screen.getByLabelText('Email'), 'nope')
    await userEvent.click(screen.getByRole('button', {name: 'Save profile'}))
    expect(await screen.findByRole('alert')).toHaveTextContent('valid email')
  })
})

describe('SettingsWindow: Shortcuts', () => {
  const chip = (label: string) => screen.getByRole('button', {name: new RegExp('^Change shortcut for ' + label)})
  const press = (opts: KeyboardEventInit & {key: string; code?: string}) => fireEvent.keyDown(window, opts)
  const WAIT = {timeout: 3000} // a one-step shortcut is kept after a short wait for a second step

  it('a second key right after the first makes a two-step shortcut (Ctrl+K, then M)', async () => {
    const {be} = setup()
    await tab('Shortcuts')
    await userEvent.click(chip('Lock the vault'))
    press({key: 'k', code: 'KeyK', ctrlKey: true})
    expect(screen.getByRole('status')).toHaveTextContent('then press another key')
    press({key: 'm', code: 'KeyM'}) // no modifier needed on the second step
    await waitFor(() => expect(be.state.settings.shortcuts).toEqual({lock_vault: 'Mod+K M'}))
    expect(chip('Lock the vault')).toHaveAccessibleName(/now Ctrl\+K M$/)
    expect(within(chip('Lock the vault')).getByText('then')).toBeInTheDocument()
  })

  it('a one-step shortcut cannot take the first step of a two-step one, and the other way round', async () => {
    setup({shortcuts: {zoom_in: 'Mod+K M'}})
    await tab('Shortcuts')
    await userEvent.click(chip('Lock the vault'))
    press({key: 'k', code: 'KeyK', ctrlKey: true})
    expect(await screen.findByRole('alert')).toHaveTextContent('Already used by “Zoom in”')
    // and a second chord may share the first step
    press({key: 'j', code: 'KeyJ', ctrlKey: true})
    press({key: 'x', code: 'KeyX'})
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull())
  })

  it('lists every action with its default and the fixed shortcuts', async () => {
    setup()
    await tab('Shortcuts')
    expect(chip('Open settings')).toHaveAccessibleName(/now Ctrl\+,$/)
    expect(chip('Zoom in')).toHaveAccessibleName(/now Ctrl\+=$/)
    expect(within(chip('Zoom in')).getAllByText(/^(Ctrl|=)$/)).toHaveLength(2) // one key cap for each key
    expect(screen.getByText('Esc')).toBeInTheDocument()
    expect(screen.getByRole('button', {name: 'Reset all'})).toBeDisabled()
  })

  it('records a new shortcut and saves only the difference from the default', async () => {
    const {be} = setup()
    await tab('Shortcuts')
    await userEvent.click(chip('Lock the vault'))
    expect(screen.getByRole('status')).toHaveTextContent('Press the new shortcut')
    press({key: 'k', code: 'KeyK', ctrlKey: true, shiftKey: true})
    await waitFor(() => expect(be.state.settings.shortcuts).toEqual({lock_vault: 'Mod+Shift+K'}), WAIT) // kept once no second step comes
    expect(chip('Lock the vault')).toHaveAccessibleName(/now Ctrl\+Shift\+K$/)
    expect(screen.queryByRole('status')).toBeNull()
  })

  it('refuses a duplicate and says which action uses it', async () => {
    const {be} = setup()
    await tab('Shortcuts')
    await userEvent.click(chip('Lock the vault'))
    press({key: ',', code: 'Comma', ctrlKey: true}) // Open settings already uses Ctrl+,
    expect(await screen.findByRole('alert')).toHaveTextContent('Already used by “Open settings”')
    expect(be.state.settings.shortcuts).toEqual({})
    expect(screen.getByRole('status')).toBeInTheDocument() // still recording: try another key
  })

  it('refuses a plain key, accepts after a valid one', async () => {
    const {be} = setup()
    await tab('Shortcuts')
    await userEvent.click(chip('Lock the vault'))
    press({key: 'k', code: 'KeyK'})
    expect(await screen.findByRole('alert')).toHaveTextContent('Ctrl, Alt')
    press({key: 'j', code: 'KeyJ', altKey: true})
    await waitFor(() => expect(be.state.settings.shortcuts).toEqual({lock_vault: 'Alt+J'}), WAIT)
  })

  it('modifier-only presses are ignored; Esc cancels without touching the window', async () => {
    const {be, onClose} = setup()
    await tab('Shortcuts')
    await userEvent.click(chip('Lock the vault'))
    press({key: 'Control', code: 'ControlLeft', ctrlKey: true})
    expect(screen.queryByRole('alert')).toBeNull()
    press({key: 'Escape', code: 'Escape'})
    expect(screen.queryByRole('status')).toBeNull()
    expect(onClose).not.toHaveBeenCalled()
    expect(be.state.settings.shortcuts).toEqual({})
  })

  it('choosing the default again removes the override; reset and reset-all work', async () => {
    const {be} = setup({shortcuts: {lock_vault: 'Alt+J', zoom_in: 'Alt+Z'}})
    await tab('Shortcuts')
    await userEvent.click(screen.getByRole('button', {name: 'Reset shortcut for Lock the vault'}))
    await waitFor(() => expect(be.state.settings.shortcuts).toEqual({zoom_in: 'Alt+Z'}))
    await userEvent.click(screen.getByRole('button', {name: 'Reset all'}))
    await waitFor(() => expect(be.state.settings.shortcuts).toEqual({}))
    await userEvent.click(chip('Lock the vault'))
    press({key: 'l', code: 'KeyL', ctrlKey: true}) // the default: nothing to store
    await waitFor(() => expect(screen.queryByRole('status')).toBeNull(), WAIT)
    expect(be.state.settings.shortcuts).toEqual({})
  })

  it('tells the global handler to stay quiet while recording', async () => {
    const {capture} = await import('../shortcuts/shortcuts')
    setup()
    await tab('Shortcuts')
    expect(capture.active).toBe(false)
    await userEvent.click(chip('Zoom out'))
    expect(capture.active).toBe(true)
    press({key: 'Escape', code: 'Escape'})
    await waitFor(() => expect(capture.active).toBe(false))
  })
})

describe('Switch', () => {
  it('toggles and exposes its state', async () => {
    const onChange = vi.fn()
    const {rerender} = render(<Switch label="Demo" checked={false} onChange={onChange} />)
    const sw = screen.getByRole('switch', {name: 'Demo'})
    expect(sw).toHaveAttribute('aria-checked', 'false')
    await userEvent.click(sw)
    expect(onChange).toHaveBeenCalledWith(true)
    rerender(<Switch label="Demo" checked onChange={onChange} />)
    expect(screen.getByRole('switch', {name: 'Demo'})).toHaveAttribute('aria-checked', 'true')
    rerender(<Switch label="Demo" checked disabled onChange={onChange} />)
    expect(screen.getByRole('switch', {name: 'Demo'})).toBeDisabled()
  })
})

describe('SettingsWindow: Highlight output', () => {
  it('is on by default in the Terminal tab and can be switched off', async () => {
    const {be} = setup()
    await tab('Terminal')
    const sw = screen.getByRole('switch', {name: 'Highlight output'})
    expect(sw).toHaveAttribute('aria-checked', 'true')
    await userEvent.click(sw)
    await waitFor(() => expect(be.state.settings.highlight).toBe(false))
  })
})

describe('SettingsWindow: Fast drawing', () => {
  it('is on by default in the Terminal tab and can be switched off', async () => {
    const {be} = setup()
    await tab('Terminal')
    const sw = screen.getByRole('switch', {name: 'Fast drawing (WebGL)'})
    expect(sw).toHaveAttribute('aria-checked', 'true')
    await userEvent.click(sw)
    await waitFor(() => expect(be.state.settings.gpu_renderer).toBe(false))
  })
})

describe('SettingsWindow: Storage', () => {
  afterEach(() => vi.restoreAllMocks())

  it('clears the cache and says what was removed', async () => {
    const {be} = setup()
    await tab('Storage')
    await userEvent.click(screen.getByRole('button', {name: 'Clear cache'}))
    expect(await screen.findByText('Cache cleared: 3 files, 5.0 KB')).toBeInTheDocument()
    expect(be.state.calls.some(c => c[0] === 'settings.clearCache')).toBe(true)
  })

  it('asks before resetting, and a "no" changes nothing', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(false)
    const {be} = setup({zoom: 140})
    await tab('Storage')
    await userEvent.click(screen.getByRole('button', {name: 'Reset settings…'}))
    expect(be.state.calls.some(c => c[0] === 'settings.reset')).toBe(false)
    expect(be.state.settings.zoom).toBe(140)
  })

  it('resets the preferences, keeps the profile and shows the defaults again', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true)
    const {be, onVault} = setup({zoom: 140, scrollback: 2000, account_name: 'Vu'})
    await tab('Storage')
    await userEvent.click(screen.getByRole('button', {name: 'Reset settings…'}))
    expect(await screen.findByText('Settings reset to their defaults')).toBeInTheDocument()
    expect(be.state.settings.zoom).toBe(100)
    expect(be.state.settings.account_name).toBe('Vu')
    expect(onVault).toHaveBeenCalled()
    await tab('General')
    expect(screen.getByLabelText('Zoom level')).toHaveTextContent('100%')
  })
})

describe('SettingsWindow: Suggest commands', () => {
  it('is on, reads no host history by default, and the history switch waits for the first one', async () => {
    const {be} = setup()
    await tab('Terminal')
    const on = screen.getByRole('switch', {name: 'Suggest commands'})
    const host = screen.getByRole('switch', {name: "Suggest from the host's shell history"})
    expect(on).toHaveAttribute('aria-checked', 'true')
    expect(host).toHaveAttribute('aria-checked', 'false')
    await userEvent.click(host)
    expect(be.state.settings.suggest_host).toBe(true)
    await userEvent.click(on)
    expect(be.state.settings.suggest).toBe(false)
    expect(screen.getByRole('switch', {name: "Suggest from the host's shell history"})).toBeDisabled()
  })
})
