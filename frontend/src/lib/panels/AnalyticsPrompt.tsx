import {useEffect, useState} from 'react'
import {Modal} from '../ui/Modal'
import {useBackend} from '../backend'
import {useToast} from '../ui/Toast'
import {errMsg} from '../util'
import {useT} from '../i18n'
import type {Settings} from '../types'

/** What is sent when the user agrees, word for word: Settings → General shows the same list. */
export function AnalyticsFacts() {
  const t = useT()
  return (
    <ul style={{margin: '8px 0 0', paddingLeft: 18}}>
      <li>{t('a random id made for this installation (not linked to you, your hosts or your vault)')}</li>
      <li>{t('the Vutty version, the operating system and the processor type')}</li>
      <li>{t('that the app was opened')}</li>
    </ul>
  )
}

/**
 * Asked once, after the vault is unlocked, in a build that reports to Google Analytics and while the user has not
 * answered yet. The answer is saved either way; closing the dialog counts as "No thanks", so it never comes back.
 */
export function AnalyticsPrompt({onSettings}: {onSettings(s: Settings): void}) {
  const be = useBackend()
  const t = useT()
  const toast = useToast()
  const [open, setOpen] = useState(false)

  // read from the backend, not from the state of the app: that holds the defaults until the saved settings arrive
  useEffect(() => {
    let live = true
    void Promise.all([be.system.info(), be.settings.get()])
      .then(([info, s]) => { if (live && info.analytics && s.analytics_consent === '') setOpen(true) })
      .catch(() => {})
    return () => { live = false }
  }, [be])

  const answer = (consent: 'yes' | 'no') => {
    setOpen(false)
    void be.settings.get()
      .then(s => be.settings.save({...s, analytics_consent: consent}))
      .then(onSettings)
      .catch(e => toast.error(errMsg(e)))
  }

  if (!open) return null
  return (
    <Modal title={t('Share anonymous usage statistics?')} onClose={() => answer('no')}
      footer={<>
        <button onClick={() => answer('no')}>{t('No thanks')}</button>
        <button className="primary" onClick={() => answer('yes')}>{t('Share')}</button>
      </>}>
      <div>{t('Vutty has no account and no server. To learn roughly how many people use it, it can tell the developer, once each time it opens:')}</div>
      <AnalyticsFacts />
      <div className="muted" style={{marginTop: 10}}>{t('Nothing else is sent: no host, key, command, file or password. Google receives it with your IP address, as with any internet request. You can change your mind any time in Settings → General.')}</div>
    </Modal>
  )
}
