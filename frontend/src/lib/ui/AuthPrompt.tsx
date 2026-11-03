import {FormEvent, useState} from 'react'
import {useBackend} from '../backend'
import {errMsg} from '../util'
import type {AuthChallenge} from '../types'

/**
 * The questions a server puts while signing in (a verification code, a Duo choice, a second password): one box for
 * each, hidden while typing unless the server says the answer may be shown. What is typed goes straight to the
 * connection and is not kept. `inline` draws it as a block in a panel instead of an overlay over a terminal.
 */
export function AuthPrompt({challenge, promptKey, onDone, onError, inline = false}: {
  challenge: AuthChallenge | null
  /** the key the connection raised the question under (the session id of a terminal) */
  promptKey: string
  /** the question was answered or cancelled: the caller forgets it */
  onDone(): void
  onError?(message: string): void
  inline?: boolean
}) {
  if (!challenge) return null
  // a new round is a new form: nothing typed carries over
  return <Form key={JSON.stringify(challenge)} challenge={challenge} promptKey={promptKey} onDone={onDone} onError={onError} inline={inline} />
}

function Form({challenge, promptKey, onDone, onError, inline}: {challenge: AuthChallenge; promptKey: string; onDone(): void; onError?(m: string): void; inline: boolean}) {
  const be = useBackend()
  const [answers, setAnswers] = useState<string[]>(() => challenge.prompts.map(() => ''))
  const [busy, setBusy] = useState(false)

  const send = async (a: string[] | null) => {
    setBusy(true)
    try {
      await be.session.answerAuth(promptKey, a)
    } catch (e) {
      onError?.(errMsg(e))
    } finally {
      setBusy(false)
      onDone()
    }
  }
  const submit = (e: FormEvent) => { e.preventDefault(); if (!busy) void send(answers) }

  const body = (
    <form onSubmit={submit} aria-label={`Sign in to ${challenge.host}`}>
      <h3 style={{marginTop: 0}}>Sign in to {challenge.host}</h3>
      {challenge.name && <div><b>{challenge.name}</b></div>}
      {challenge.instruction && <div className="muted" style={{whiteSpace: 'pre-wrap'}}>{challenge.instruction}</div>}
      {challenge.prompts.map((p, i) => (
        <label key={i} className="field">
          <span>{p.text.replace(/\s*:\s*$/, '')}</span>
          <input aria-label={p.text.replace(/\s*:\s*$/, '')} type={p.echo ? 'text' : 'password'} autoComplete="off" autoFocus={i === 0} spellCheck={false}
            value={answers[i]} onChange={e => setAnswers(a => a.map((x, j) => (j === i ? e.target.value : x)))} />
        </label>
      ))}
      <div className="row">
        <button type="submit" className="primary" disabled={busy || answers.every(a => a === '')}>Continue</button>
        <button type="button" disabled={busy} onClick={() => void send(null)}>Cancel</button>
      </div>
    </form>
  )
  return inline
    ? <div className="notice" role="alertdialog" aria-label="Sign-in question">{body}</div>
    : <div className="overlay" role="alertdialog" aria-label="Sign-in question"><div className="card">{body}</div></div>
}
