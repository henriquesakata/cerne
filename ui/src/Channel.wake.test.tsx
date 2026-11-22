import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, render, renderHook, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import { ChannelLine } from './Channel'
import { I18nProvider, useT, type Lang } from './i18n'
import { WAKE_REASONS, type ChannelMessage, type ChannelTarget, type WakeReason } from './api'

// F-0491: the server says why an idle session was not woken (the Wake* codes
// of internal/app/channelwake.go); the target line says it in the viewer's
// language.

afterEach(() => {
  cleanup()
  localStorage.clear()
})

const message = (targets: ChannelTarget[]): ChannelMessage => ({
  seq: 7,
  type: 'message',
  nick: 'W-0001',
  body: 'W-0002 libera o rooms.go?',
  time: '2026-11-21T14:50:00Z',
  state: 'not_delivered',
  directed: true,
  targets,
})

const line = (lang: Lang, tg: ChannelTarget) => {
  localStorage.setItem('cerne-lang', lang)
  render(
    <I18nProvider>
      <ChannelLine m={message([tg])} />
    </I18nProvider>,
  )
  return screen.getByRole('listitem')
}

describe('ChannelLine — why a target was not woken (F-0491)', () => {
  // F-0491 crit. 2
  it('F-0491 crit. 2: a session in bypass says it gets the message at its next contact point, in pt-BR and en', () => {
    expect(line('pt-BR', { work: 'W-0002', state: 'not_delivered', reason: 'bypass' })).toHaveTextContent(
      'W-0002 ainda não recebeu — em modo bypass, recebe no próximo ponto de contato',
    )
    cleanup()
    expect(line('en', { work: 'W-0002', state: 'not_delivered', reason: 'bypass' })).toHaveTextContent(
      'W-0002 has not received it yet — in bypass mode, it gets it at its next contact point',
    )
  })

  // F-0491 crit. 2
  it('F-0491 crit. 2: an unknown mode reads as such, not as bypass', () => {
    expect(line('pt-BR', { work: 'W-0002', state: 'not_delivered', reason: 'unknown_mode' })).toHaveTextContent(
      'modo de permissão desconhecido, recebe no próximo ponto de contato',
    )
  })

  // F-0491 crit. 7
  it('F-0491 crit. 7: a harness that cannot wake an idle session says so, and an ended session says it ended', () => {
    expect(line('pt-BR', { work: 'W-0003', state: 'not_delivered', reason: 'no_wake' })).toHaveTextContent(
      'o harness dela não acorda sessão parada; recebe no próximo ponto de contato',
    )
    cleanup()
    for (const reason of ['no_registry', 'socket_missing', 'connection_refused'] as const) {
      expect(line('pt-BR', { work: 'W-0003', state: 'not_delivered', reason })).toHaveTextContent('sessão encerrada')
      cleanup()
      expect(line('en', { work: 'W-0003', state: 'not_delivered', reason })).toHaveTextContent('session ended')
      cleanup()
    }
  })

  // F-0491 crit. 9
  it('F-0491 crit. 9: cooldown, cap and chain each say why, and that the message still arrives', () => {
    const want: Record<'cooldown' | 'cap' | 'chain', [string, string]> = {
      cooldown: ['acordada há menos de 2 min', 'woken less than 2 min ago'],
      cap: ['este post já acordou 5 sessões', 'this post already woke 5 sessions'],
      chain: ['post de um turno que um agente abriu não acorda ninguém', 'a post from a turn an agent opened wakes no one'],
    }
    for (const [reason, [pt, en]] of Object.entries(want) as [WakeReason, [string, string]][]) {
      expect(line('pt-BR', { work: 'W-0002', state: 'not_delivered', reason })).toHaveTextContent(`${pt}; recebe no próximo ponto de contato`)
      cleanup()
      expect(line('en', { work: 'W-0002', state: 'not_delivered', reason })).toHaveTextContent(`${en}; it gets it at its next contact point`)
      cleanup()
    }
  })

  // F-0491 review item 4
  it('F-0491: an untrusted socket says the session was not woken', () => {
    expect(line('en', { work: 'W-0002', state: 'not_delivered', reason: 'untrusted_socket' })).toHaveTextContent(
      'untrusted inbox socket, not woken; it gets it at its next contact point',
    )
  })

  it('F-0491: with no reason, and once delivered, the line is as before', () => {
    expect(line('pt-BR', { work: 'W-0002', state: 'not_delivered' }).textContent?.trim()).toBe('W-0002 ainda não recebeu')
    cleanup()
    expect(line('pt-BR', { work: 'W-0002', state: 'delivered', reason: 'bypass' }).textContent?.trim()).toBe('W-0002 recebeu — sem resposta')
  })
})

// F-0491 crit. 2 and 7: every code the server serves has a label in both
// languages, and they differ.
describe('i18n — wake reasons (F-0491)', () => {
  const channel = (lang: Lang) => {
    localStorage.setItem('cerne-lang', lang)
    const { result } = renderHook(() => useT(), { wrapper: I18nProvider })
    return result.current.t.channel
  }

  it('F-0491 crit. 2/7: pt-BR and en label every wake reason, with different texts', () => {
    const pt = channel('pt-BR')
    const en = channel('en')
    expect(WAKE_REASONS.length).toBe(11)
    for (const r of WAKE_REASONS) {
      expect(pt.wakeReason[r], r).toMatch(/\S/)
      expect(en.wakeReason[r], r).toMatch(/\S/)
      expect(pt.wakeReason[r], r).not.toBe(en.wakeReason[r])
    }
  })
})
