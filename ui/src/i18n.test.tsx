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
    [360 * DAY, 'há 1 ano'],
    [364 * DAY, 'há 1 ano'],
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
    [360 * DAY, '1 year ago'],
    [364 * DAY, '1 year ago'],
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
})
