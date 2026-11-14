import { afterEach, describe, expect, it } from 'vitest'
import { renderHook } from '@testing-library/react'
import { I18nProvider, useT, type Lang } from './i18n'

// age devolve o formatador t.notif.age da tabela do idioma, pelo provider
// real (sem export novo): o idioma vem do localStorage, como no app.
function age(lang: Lang): (min: number) => string {
  localStorage.setItem('cerne-lang', lang)
  const { result } = renderHook(() => useT(), { wrapper: I18nProvider })
  return result.current.t.notif.age
}

afterEach(() => localStorage.clear())

const DAY = 1440

describe('t.notif.age (F-0310)', () => {
  it.each([
    [0, 'agora'],
    [1, 'há 1 minuto'],
    [59, 'há 59 minutos'],
    [60, 'há 1 hora'],
    [119, 'há 1 hora'],
    [1439, 'há 23 horas'],
    [1440, 'há 1 dia'],
    [2880, 'há 2 dias'],
    [29 * DAY, 'há 29 dias'],
    [30 * DAY, 'há 1 mês'],
    [59 * DAY, 'há 1 mês'],
    [60 * DAY, 'há 2 meses'],
    [330 * DAY, 'há 11 meses'],
    [359 * DAY, 'há 11 meses'],
    // C13: crit. 6 conta o ano como 365 dias — 360-364 d ainda são meses.
    [360 * DAY, 'há 12 meses'],
    [364 * DAY, 'há 12 meses'],
    [365 * DAY, 'há 1 ano'],
    [730 * DAY, 'há 2 anos'],
    [1094 * DAY, 'há 2 anos'],
  ])('critérios 1-6: régua de seis faixas e fronteiras exatas (pt-BR) — %i min → %s', (min, want) => {
    expect(age('pt-BR')(min)).toBe(want)
  })

  it.each([
    [0, 'now'],
    [1, '1 minute ago'],
    [59, '59 minutes ago'],
    [60, '1 hour ago'],
    [119, '1 hour ago'],
    [1439, '23 hours ago'],
    [1440, '1 day ago'],
    [2880, '2 days ago'],
    [29 * DAY, '29 days ago'],
    [30 * DAY, '1 month ago'],
    [59 * DAY, '1 month ago'],
    [60 * DAY, '2 months ago'],
    [330 * DAY, '11 months ago'],
    [359 * DAY, '11 months ago'],
    [360 * DAY, '12 months ago'],
    [364 * DAY, '12 months ago'],
    [365 * DAY, '1 year ago'],
    [730 * DAY, '2 years ago'],
    [1094 * DAY, '2 years ago'],
  ])('critérios 1-6: régua de seis faixas e fronteiras exatas (en) — %i min → %s', (min, want) => {
    expect(age('en')(min)).toBe(want)
  })

  it('critério 7: singular e plural por unidade, sem abreviação', () => {
    const samples = [0, 1, 2, 59, 60, 120, 1440, 2880, 30 * DAY, 60 * DAY, 360 * DAY, 730 * DAY]
    for (const lang of ['pt-BR', 'en'] as const) {
      const f = age(lang)
      // Fronteira por letra Unicode: o \b do JS é ASCII e casaria o "h" de "há".
      for (const min of samples) expect(f(min)).not.toMatch(/(?<!\p{L})(min|h|d)(?!\p{L})/u)
    }
  })

  // C3: um carimbo ausente ou corrompido no storage chega aqui como NaN; o
  // formatador não pode lançar (derrubava a tela do Backlog inteira).
  it.each([NaN, Infinity, -Infinity])('C3: idade não finita (%s) vira "—" sem lançar', (min) => {
    expect(age('pt-BR')(min)).toBe('—')
    expect(age('en')(min)).toBe('—')
  })
})

// leaves achata as folhas em caminho → texto; uma função é chamada com 'x'.
const leaves = (o: object, at = ''): Record<string, string> =>
  Object.fromEntries(
    Object.entries(o).flatMap(([k, v]) =>
      typeof v === 'object' && v !== null
        ? Object.entries(leaves(v, `${at}${k}.`))
        : [[`${at}${k}`, typeof v === 'function' ? String(v('x')) : String(v)]],
    ),
  )

// F-0331 crit. 10: o bloco "Nome do projeto" existe nas duas línguas, com
// textos distintos entre elas, e o rótulo diz projeto — não o nome pessoal no
// canal (F-0333 crit. 5).
describe('t.settings.projectName (F-0331 crit. 10)', () => {
  const messages = (lang: Lang) => {
    localStorage.setItem('cerne-lang', lang)
    const { result } = renderHook(() => useT(), { wrapper: I18nProvider })
    return result.current.t.settings
  }

  it('mesmas chaves, textos diferentes, "projeto"/"project" e distinto de Your name in the channel', () => {
    const pt = messages('pt-BR')
    const en = messages('en')
    const lp = leaves(pt.projectName)
    const le = leaves(en.projectName)
    expect(Object.keys(lp).sort()).toEqual(Object.keys(le).sort())
    for (const k of Object.keys(lp)) expect(lp[k], k).not.toBe(le[k])
    expect(pt.projectName.title.toLowerCase()).toContain('projeto')
    expect(en.projectName.title.toLowerCase()).toContain('project')
    // Crítica C8: sem a chave pt, o not.toBe passaria contra undefined.
    expect(pt.text['Your name in the channel']).toBeDefined()
    expect(pt.projectName.title).not.toBe(pt.text['Your name in the channel'])
    expect(en.projectName.title).not.toBe('Your name in the channel')
  })
})

// F-0333 crit. 1 e 2: o rótulo pt-BR do channel.human_name marca a pessoa, e a
// ajuda diz, nesta ordem, que é você quem envia, em todos os projetos, que não
// é o nome do projeto (que fica no campo “Nome do projeto”) e o que vale vazio.
describe("t.settings.text de channel.human_name (F-0333)", () => {
  // O texto en do registro Go: é a chave do catálogo pt-BR, então uma chave
  // presente prova que este literal é o do registro.
  const en =
    "The name that signs the messages you send from the UI on a project's permanent channel. It applies to every project on this machine. It is not the project's name, which is set in the “Project name” field of the Settings screen. Empty uses your OS user name."
  const settings = (lang: Lang) => {
    localStorage.setItem('cerne-lang', lang)
    const { result } = renderHook(() => useT(), { wrapper: I18nProvider })
    return result.current.t.settings
  }

  it('título "Seu nome no canal"', () => {
    expect(settings('pt-BR').text['Your name in the channel']).toBe('Seu nome no canal')
  })

  it('descrição com os trechos do critério 2, em ordem', () => {
    const d = (settings('pt-BR').text[en] ?? '').toLowerCase()
    // Cada trecho é procurado depois do fim do anterior: "nome do projeto" é o
    // campo citado, não o pedaço de "não é o nome do projeto".
    let from = 0
    for (const part of ['você envia', 'todos os projetos', 'não é o nome do projeto', '“nome do projeto”', 'usuário do sistema']) {
      const i = d.indexOf(part, from)
      expect(i, part).toBeGreaterThanOrEqual(from)
      from = i + part.length
    }
  })

  // Crítica C7: o campo citado é o título do bloco da F-0331, lido do
  // dicionário, não um literal que envelhece se o título mudar.
  it('cita entre aspas o título do bloco "Nome do projeto" de cada língua', () => {
    const pt = settings('pt-BR')
    expect(pt.text[en]).toContain(`“${pt.projectName.title}”`)
    expect(en).toContain(`“${settings('en').projectName.title}”`)
  })
})

// F-0334 crit. 8: os textos da tela de pendências existem nas duas línguas,
// com as mesmas chaves e textos diferentes entre elas.
describe('t.pendings (F-0334 crit. 8)', () => {
  const messages = (lang: Lang) => {
    localStorage.setItem('cerne-lang', lang)
    const { result } = renderHook(() => useT(), { wrapper: I18nProvider })
    return result.current.t
  }

  it('mesmas chaves e textos diferentes em pt-BR e en', () => {
    const lp = leaves(messages('pt-BR').pendings)
    const le = leaves(messages('en').pendings)
    expect(Object.keys(lp).length).toBeGreaterThan(0)
    expect(Object.keys(lp).sort()).toEqual(Object.keys(le).sort())
    for (const k of Object.keys(lp)) expect(lp[k], k).not.toBe(le[k])
  })

  it('os itens do menu existem nas duas línguas e diferem', () => {
    const pt = messages('pt-BR').nav
    const en = messages('en').nav
    expect([pt.pendings, pt.ideas]).toEqual(['Pendências', 'Ideias'])
    expect([en.pendings, en.ideas]).toEqual(['Pendings', 'Ideas'])
  })
})

// F-0415 crit. 10: o texto do destino do arraste existe nas duas línguas.
describe('t.kanban.drag hints (F-0415 crit. 10)', () => {
  const drag = (lang: Lang) => {
    localStorage.setItem('cerne-lang', lang)
    const { result } = renderHook(() => useT(), { wrapper: I18nProvider })
    return result.current.t.kanban.drag
  }

  it('Solte para aprovar/aceitar em pt-BR e Drop to approve/accept em en', () => {
    expect(drag('pt-BR')).toMatchObject({ hintApprove: 'Solte para aprovar', hintAccept: 'Solte para aceitar' })
    expect(drag('en')).toMatchObject({ hintApprove: 'Drop to approve', hintAccept: 'Drop to accept' })
  })
})
