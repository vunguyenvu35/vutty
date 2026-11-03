import {FormEvent, useState} from 'react'
import {Modal} from '../ui/Modal'
import type {Variable} from './variables'

/** The blanks of a snippet, asked before it runs: one box each, a default when the snippet has one. */
export function FillVariables({title, variables, verb, onSubmit, onAsWritten, onClose}: {
  title: string
  variables: Variable[]
  /** what the main button does: "Run" or "Paste" */
  verb: string
  onSubmit(values: Record<string, string>): void
  /** use the script exactly as it is (the {{…}} was not meant as a blank) */
  onAsWritten?(): void
  onClose(): void
}) {
  const [values, setValues] = useState<Record<string, string>>({})
  const submit = (e: FormEvent) => { e.preventDefault(); onSubmit(values) }
  return (
    <Modal title={title} onClose={onClose}>
      <form onSubmit={submit} aria-label="Fill in the snippet">
        <p className="muted" style={{marginTop: 0}}>This snippet has blanks. What you type is put into the script as plain text.</p>
        {variables.map((v, i) => (
          <label key={v.name} className="field">
            <span>{v.name}{v.def ? ` (default: ${v.def})` : ''}</span>
            <input aria-label={v.name} autoFocus={i === 0} spellCheck={false} placeholder={v.def} autoComplete="off"
              value={values[v.name] ?? ''} onChange={e => setValues(x => ({...x, [v.name]: e.target.value}))} />
          </label>
        ))}
        <div className="row" style={{justifyContent: 'flex-end', marginTop: 14}}>
          {onAsWritten && <button type="button" className="link" onClick={onAsWritten} style={{marginRight: 'auto', flex: 'none'}}>Use as written</button>}
          <button type="button" onClick={onClose} style={{flex: 'none'}}>Cancel</button>
          <button type="submit" className="primary" style={{flex: 'none'}}>{verb}</button>
        </div>
      </form>
    </Modal>
  )
}
