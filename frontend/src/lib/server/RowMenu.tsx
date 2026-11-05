import {useEffect} from 'react'
import {createPortal} from 'react-dom'
import {useT} from '../i18n'
import {useMenuAnchor} from './KubernetesShared'

/** the ⋮ of a row: the actions that are not the main one, in a small menu */
export function RowMenu({name, items}: {name: string; items: {label: string; danger?: boolean; disabled?: boolean; run(): void}[]}) {
  const t = useT()
  const {btn, menu, at, setAt, toggle, style} = useMenuAnchor(180)
  useEffect(() => {
    if (!at) return
    const away = (e: MouseEvent) => { if (!(e.target as Element).closest?.('.k8s-menu') && !btn.current?.contains(e.target as Node)) setAt(null) }
    const esc = (e: KeyboardEvent) => { if (e.key === 'Escape') setAt(null) }
    document.addEventListener('mousedown', away)
    window.addEventListener('keydown', esc)
    return () => { document.removeEventListener('mousedown', away); window.removeEventListener('keydown', esc) }
  }, [at]) // eslint-disable-line react-hooks/exhaustive-deps
  return (
    <>
      <button ref={btn} className="row-more" aria-haspopup="menu" aria-expanded={!!at} aria-label={t('More actions for {name}', {name})} title={t('More actions')} onClick={toggle}>⋮</button>
      {at && createPortal(
        <div ref={menu} className="k8s-menu row-menu" role="menu" aria-label={t('More actions for {name}', {name})} style={style}>
          {items.map(i => <button key={i.label} role="menuitem" className={'k8s-mi' + (i.danger ? ' danger' : '')} disabled={i.disabled} onClick={() => { setAt(null); i.run() }}>{i.label}</button>)}
        </div>, document.body)}
    </>
  )
}
