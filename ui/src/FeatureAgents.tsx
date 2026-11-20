import { useState, type FormEvent } from 'react'
import {
  fetchFeatureAgents,
  postChannel,
  readChannel,
  type AgentRole,
  type AgentSession,
  type ChannelTarget,
} from './api'
import { ChannelLine, useChannelFollow } from './Channel'
import { FixBlock } from './Harness'
import { useT } from './i18n'
import { LoadNotice, LoadPending, useLiveLoad } from './liveLoad'

// FeatureAgents (F-0486): the sessions of this machine that worked on the card
// — who opened it, wrote each document, implemented or reviewed it — with
// where each one is, and a collapsed chat that reaches only the checked
// sessions through the project channel. A session waiting for the human runs
// no hook and reads nothing: the page says so and shows how to open it.
export default function FeatureAgents({ id }: { id: string }) {
  const { t } = useT()
  const load = useLiveLoad(() => fetchFeatureAgents(id), [id])
  const [open, setOpen] = useState(false)
  const [unchecked, setUnchecked] = useState<ReadonlySet<string>>(new Set())

  if (load.data === null) {
    return (
      <section className="agents">
        <h2>{t.agents.title}</h2>
        <LoadPending load={load} message={t.agents.loadError} />
      </section>
    )
  }
  if ('unavailable' in load.data) {
    return (
      <section className="agents">
        <h2>{t.agents.title}</h2>
        <p className="muted">{t.agents.localOnly}</p>
      </section>
    )
  }
  const sessions = load.data.sessions
  const toggle = (s: string) =>
    setUnchecked((u) => {
      const n = new Set(u)
      if (!n.delete(s)) n.add(s)
      return n
    })
  const selected = sessions.filter((s) => !unchecked.has(s.session)).map((s) => s.session)

  return (
    <section className="agents">
      <h2>{t.agents.title}</h2>
      <LoadNotice load={load} />
      {sessions.length === 0 ? (
        <p className="muted">{t.agents.none}</p>
      ) : (
        <>
          <p className="muted">{t.agents.intro}</p>
          <ul className="agents-list">
            {sessions.map((s) => (
              <AgentRow key={s.session} s={s} checked={!unchecked.has(s.session)} onToggle={() => toggle(s.session)} />
            ))}
          </ul>
        </>
      )}
      <button type="button" className="queue-btn" aria-expanded={open} onClick={() => setOpen((o) => !o)}>
        {open ? t.agents.closeChat : t.agents.openChat}
      </button>
      {open && <AgentChat id={id} sessions={sessions} selected={selected} />}
    </section>
  )
}

function sessionTitle(s: AgentSession, untitled: string): string {
  return s.info.title || (s.info.found ? untitled : s.session.slice(0, 8))
}

// resumeCommand opens the session again from its folder. Only a Claude Code
// session resumes with claude.
function resumeCommand(s: AgentSession): string | null {
  if (s.harness !== 'claude-code') return null
  const resume = `claude --resume ${s.session}`
  return s.info.cwd ? `cd "${s.info.cwd}" && ${resume}` : resume
}

function AgentRow({ s, checked, onToggle }: { s: AgentSession; checked: boolean; onToggle: () => void }) {
  const { t, lang } = useT()
  const title = sessionTitle(s, t.agents.untitled)
  const role = (r: AgentRole) => {
    const what = t.agents.role(r.kind, r.work ?? '', r.work_title ?? '')
    return r.agent ? `${what} (${t.agents.subagent(r.agent_type ?? '')})` : what
  }
  const resume = resumeCommand(s)
  return (
    <li className="agents-row">
      <label>
        <input type="checkbox" checked={checked} onChange={onToggle} aria-label={t.agents.pick(title)} />{' '}
        <strong>{title}</strong>
      </label>
      <p>{s.roles.map(role).join(' · ')}</p>
      {s.info.found ? (
        <p className="muted">
          {s.info.cwd && <code>{s.info.cwd}</code>}
          {s.info.branch && (
            <>
              {' · '}
              {t.agents.branch} <code>{s.info.branch}</code>
            </>
          )}
          {s.info.pr && (
            <>
              {' · '}
              <a href={s.info.pr} target="_blank" rel="noreferrer">
                {s.info.pr}
              </a>
            </>
          )}
          {s.info.last && (
            <>
              {' · '}
              {t.agents.lastActivity(new Date(s.info.last).toLocaleString(lang))}
            </>
          )}
        </p>
      ) : (
        <p className="muted">{t.agents.noTranscript(s.session, s.harness)}</p>
      )}
      {resume && <FixBlock command={resume} label={t.agents.resume} />}
    </li>
  )
}

function AgentChat({ id, sessions, selected }: { id: string; sessions: AgentSession[]; selected: string[] }) {
  const { t } = useT()
  const load = useLiveLoad(() => readChannel(false, 0, undefined, id), [id])
  useChannelFollow(load.data?.version, load.reload)
  const [input, setInput] = useState('')
  const [sending, setSending] = useState(false)
  const [sendError, setSendError] = useState<string | null>(null)

  const byShort = (tg: ChannelTarget) => sessions.find((s) => tg.session && s.session.startsWith(tg.session))
  const targetName = (tg: ChannelTarget) => {
    const s = byShort(tg)
    return s ? sessionTitle(s, t.agents.untitled) : tg.work
  }
  const blocked = sessions.length === 0 ? t.agents.noSessionToSend : selected.length === 0 ? t.agents.noRecipient : null

  const send = (e: FormEvent) => {
    e.preventDefault()
    const body = input.trim()
    if (body === '' || sending || blocked) return
    setSending(true)
    setSendError(null)
    postChannel(body, id, selected)
      .then(() => {
        setInput('')
        return load.reload()
      })
      .catch((err: unknown) => setSendError(err instanceof Error ? err.message : String(err)))
      .finally(() => setSending(false))
  }

  return (
    <div className="agents-chat">
      {load.data === null ? (
        <LoadPending load={load} message={t.channel.loadError} />
      ) : (
        <div className="backlog-chat-history">
          {load.data.messages.length === 0 && <p className="muted">{t.agents.threadEmpty}</p>}
          {load.data.messages.map((m) => {
            const unreached = (m.targets ?? []).filter((tg) => tg.state === 'not_delivered').map(byShort)
            return (
              <ChannelLine
                key={m.seq}
                m={m}
                targetName={targetName}
                extra={unreached.map(
                  (s) =>
                    s && (
                      <div key={s.session} className="agents-unreached">
                        <p className="muted">{t.agents.notReached(sessionTitle(s, t.agents.untitled))}</p>
                        {resumeCommand(s) && <FixBlock command={resumeCommand(s)!} label={t.agents.resume} />}
                      </div>
                    ),
                )}
              />
            )
          })}
        </div>
      )}
      <form className="backlog-chat-form" onSubmit={send}>
        <textarea
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder={t.agents.placeholder}
          rows={3}
          disabled={blocked !== null}
        />
        <button type="submit" disabled={sending || input.trim() === '' || blocked !== null}>
          {sending ? t.channel.sending : t.channel.send}
        </button>
      </form>
      {blocked && <p className="muted">{blocked}</p>}
      {sendError && <p className="status error">{t.channel.sendError(sendError)}</p>}
    </div>
  )
}
