import {KeyboardEvent, useEffect, useId, useLayoutEffect, useRef, useState} from 'react'
import {createPortal} from 'react-dom'
import {useT} from '../i18n'
import type {SelectOption} from './Select'

interface Props {
  /** accessible name of the control */
  label: string
  /** the ticked values; none ticked means "all" */
  values: string[]
  onChange(values: string[]): void
  options: SelectOption[]
  /** what the first row (which clears every tick) and the button say when nothing is ticked */
  allLabel: string
  className?: string
}

/**
 * Like Select, but several options can be ticked: a click ticks or unticks one and the menu stays open. The first row, with
 * `allLabel`, clears every tick (nothing ticked is "all"). Keyboard: Arrow keys move, Enter or Space tick, Esc closes.
 */
export function MultiSelect({label, values, onChange, options, allLabel, className = ''}: Props) {
  const t = useT()
  const id = useId()
  const trigger = useRef<HTMLButtonElement>(null)
  const menu = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [active, setActive] = useState(0) // 0 is the "all" row, the options follow
  const [pos, setPos] = useState<{left: number; width: number; top?: number; bottom?: number; maxHeight: number}>({left: 0, width: 0, maxHeight: 280})

  const ticked = options.filter(o => values.includes(o.value))
  const summary = ticked.length === 0 ? allLabel : ticked.length <= 2 ? ticked.map(o => o.label).join(', ') : t('{n} selected', {n: ticked.length})
  const rows = options.length + 1

  const toggle = (value: string) => onChange(values.includes(value) ? values.filter(v => v !== value) : [...values, value])
  const pick = (row: number) => { if (row === 0) onChange([]); else if (!options[row - 1].disabled) toggle(options[row - 1].value) }
  const close = (refocus = true) => { setOpen(false); if (refocus) trigger.current?.focus() }

  useLayoutEffect(() => {
    if (!open || !trigger.current) return
    const r = trigger.current.getBoundingClientRect()
    const below = window.innerHeight - r.bottom - 12
    const above = r.top - 12
    const up = below < 180 && above > below
    setPos({
      left: r.left, width: Math.max(r.width, 220),
      ...(up ? {bottom: window.innerHeight - r.top + 6} : {top: r.bottom + 6}),
      maxHeight: Math.max(120, Math.min(320, up ? above : below)),
    })
    menu.current?.focus()
  }, [open])

  useEffect(() => {
    if (!open) return
    const outside = (e: MouseEvent) => {
      const n = e.target as Node
      if (!menu.current?.contains(n) && !trigger.current?.contains(n)) setOpen(false)
    }
    const away = () => setOpen(false)
    document.addEventListener('mousedown', outside)
    window.addEventListener('resize', away)
    window.addEventListener('blur', away)
    return () => {
      document.removeEventListener('mousedown', outside)
      window.removeEventListener('resize', away)
      window.removeEventListener('blur', away)
    }
  }, [open])

  useEffect(() => {
    if (open) document.getElementById(`${id}-${active}`)?.scrollIntoView?.({block: 'nearest'})
  }, [open, active, id])

  const openMenu = () => { setActive(0); setOpen(true) }
  const onTriggerKey = (e: KeyboardEvent) => {
    if (['ArrowDown', 'ArrowUp', 'Enter', ' '].includes(e.key)) { e.preventDefault(); openMenu() }
  }
  const onMenuKey = (e: KeyboardEvent) => {
    switch (e.key) {
      case 'ArrowDown': e.preventDefault(); setActive(a => Math.min(rows - 1, a + 1)); break
      case 'ArrowUp': e.preventDefault(); setActive(a => Math.max(0, a - 1)); break
      case 'Home': e.preventDefault(); setActive(0); break
      case 'End': e.preventDefault(); setActive(rows - 1); break
      case 'Enter': case ' ': e.preventDefault(); pick(active); break
      case 'Escape': e.preventDefault(); e.stopPropagation(); close(); break
      case 'Tab': close(false); break
    }
  }

  const tick = <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m5 12 5 5 9-10" /></svg>

  return (
    <div className={'select-wrap ' + className}>
      <button ref={trigger} type="button" className={'select-trigger' + (open ? ' open' : '')} role="combobox"
        aria-haspopup="listbox" aria-expanded={open} aria-controls={open ? id : undefined} aria-label={`${label}, ${summary}`}
        onClick={() => (open ? close(false) : openMenu())} onKeyDown={onTriggerKey}>
        <span className="select-value">{summary}</span>
        <svg className="select-chevron" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6" /></svg>
      </button>
      {open && createPortal(
        <div ref={menu} id={id} className="select-menu" role="listbox" aria-multiselectable="true" aria-label={label} tabIndex={-1}
          aria-activedescendant={`${id}-${active}`} onKeyDown={onMenuKey}
          style={{left: pos.left, width: pos.width, top: pos.top, bottom: pos.bottom, maxHeight: pos.maxHeight}}>
          <div id={`${id}-0`} role="option" aria-selected={ticked.length === 0} className={'select-option' + (active === 0 ? ' active' : '') + (ticked.length === 0 ? ' selected' : '')}
            onMouseMove={() => setActive(0)} onMouseDown={e => e.preventDefault()} onClick={() => pick(0)}>
            <span className="opt-text">{allLabel}</span>
            {ticked.length === 0 && tick}
          </div>
          {options.map((o, i) => {
            const on = values.includes(o.value)
            return (
              <div key={o.value} id={`${id}-${i + 1}`} role="option" aria-selected={on} aria-disabled={o.disabled || undefined}
                className={'select-option' + (active === i + 1 ? ' active' : '') + (on ? ' selected' : '') + (o.disabled ? ' disabled' : '')}
                onMouseMove={() => !o.disabled && setActive(i + 1)} onMouseDown={e => e.preventDefault()} onClick={() => pick(i + 1)}>
                <span className="opt-text">{o.label}{o.hint && <small>{o.hint}</small>}</span>
                {on && tick}
              </div>
            )
          })}
        </div>, document.body)}
    </div>
  )
}
