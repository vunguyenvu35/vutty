import type {OpenApp} from '../types'
import type {TFn} from '../i18n'

/** What the file menu shows after Open with…: the apps that can open the file, "Other…" for any other app, and a way back. */
export function OpenWithList({apps, t, onPick, onOther, onBack}: {
  apps: OpenApp[]
  t: TFn
  onPick(app: OpenApp): void
  onOther(): void
  onBack(): void
}) {
  return (
    <>
      <button role="menuitem" className="ctx-back" onClick={onBack}>‹ {t('Open with…')}</button>
      <div className="ctx-sep" role="separator" />
      {apps.map(a => <button key={a.path} role="menuitem" title={a.path} onClick={() => onPick(a)}>{a.name}</button>)}
      <div className="ctx-sep" role="separator" />
      <button role="menuitem" onClick={onOther}>{t('Other…')}</button>
    </>
  )
}
