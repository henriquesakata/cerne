import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  DATE_LOCALE_KEY,
  TIME_ZONE_KEY,
  detectLocale,
  detectTimeZone,
  formatInstant,
  isValidLocale,
  isValidTimeZone,
  readStored,
  resolvePrefs,
  writeStored,
} from './datetime'

// The stored names, spelled out: a test that sets a "KEY" next to a value with
// some entropy trips gitleaks, which reads the whole history (N-0072).
const LOCALE_STORE = 'cerne-date-locale'
const ZONE_STORE = 'cerne-time-zone'

const ISO = '2026-11-21T14:43:37Z'

// languages simula navigator.languages (e navigator.language, o primeiro dela).
function languages(list: string[]) {
  vi.spyOn(navigator, 'languages', 'get').mockReturnValue(list)
  vi.spyOn(navigator, 'language', 'get').mockReturnValue(list[0] ?? '')
}

afterEach(() => {
  vi.restoreAllMocks()
  localStorage.clear()
})

describe('formatInstant (F-0493)', () => {
  // F-0493 crit. 1
  it('F-0493 crit. 1: o locale decide o formato, no mesmo fuso', () => {
    const us = { locale: 'en-US', timeZone: 'UTC' }
    const gb = { locale: 'en-GB', timeZone: 'UTC' }
    const br = { locale: 'pt-BR', timeZone: 'UTC' }
    expect(formatInstant(ISO, 'short', us)).toBe('11/21, 02:43 PM')
    expect(formatInstant(ISO, 'short', gb)).toBe('21/11, 14:43')
    expect(formatInstant(ISO, 'short', br)).toBe('21/11, 14:43')
    expect(formatInstant(ISO, 'full', gb)).not.toBe(formatInstant(ISO, 'full', us))
    expect(formatInstant(ISO, 'full', br)).not.toBe(formatInstant(ISO, 'full', us))
  })

  // F-0493 crit. 1
  it('F-0493 crit. 1: o fuso escolhido desloca a hora do dia', () => {
    expect(formatInstant(ISO, 'time', { locale: 'pt-BR', timeZone: 'UTC' })).toBe('14:43')
    expect(formatInstant(ISO, 'time', { locale: 'pt-BR', timeZone: 'America/Sao_Paulo' })).toBe('11:43')
    expect(formatInstant(ISO, 'time', { locale: 'en-US', timeZone: 'Asia/Tokyo' })).toBe('11:43 PM')
  })

  // F-0493 crit. 1
  it('F-0493 crit. 1: datetime traz o ano que o short do canal não traz', () => {
    const out = formatInstant(ISO, 'datetime', { locale: 'en-GB', timeZone: 'UTC' })
    expect(out).toBe('21/11/2026, 14:43')
  })

  // F-0493 crit. 2
  it('F-0493 crit. 2: o full diz o fuso, com os segundos', () => {
    expect(formatInstant(ISO, 'full', { locale: 'pt-BR', timeZone: 'UTC' })).toBe('21/11/2026, 14:43:37 UTC')
    expect(formatInstant(ISO, 'full', { locale: 'pt-BR', timeZone: 'America/Sao_Paulo' })).toMatch(
      /^21\/11\/2026, 11:43:37 (BRT|GMT-3)$/,
    )
  })

  // F-0493 crit. 5
  it('F-0493 crit. 5: instante vazio ou inválido dá texto vazio, sem lançar', () => {
    const p = { locale: 'pt-BR', timeZone: 'UTC' }
    for (const style of ['short', 'datetime', 'time', 'full'] as const) {
      expect(formatInstant('', style, p)).toBe('')
      expect(formatInstant('não é data', style, p)).toBe('')
    }
  })

  // F-0493 crit. 5
  it('F-0493 crit. 5: sem fuso resolvido usa o que o navegador assume', () => {
    expect(formatInstant(ISO, 'time', { locale: 'pt-BR' })).toMatch(/^\d{2}:\d{2}$/)
  })
})

describe('validade (F-0493)', () => {
  it('F-0493 crit. 9: locale e fuso que o navegador não conhece são inválidos', () => {
    expect(isValidLocale('pt-BR')).toBe(true)
    expect(isValidLocale('en-GB')).toBe(true)
    expect(isValidLocale('xx-INVALID')).toBe(false)
    expect(isValidLocale('não é locale')).toBe(false)
    expect(isValidLocale('')).toBe(false)
    expect(isValidTimeZone('America/Sao_Paulo')).toBe(true)
    expect(isValidTimeZone('UTC')).toBe(true)
    expect(isValidTimeZone('Lua/Base')).toBe(false)
    expect(isValidTimeZone('')).toBe(false)
  })
})

describe('resolvePrefs (F-0493)', () => {
  // F-0493 crit. 1
  it('F-0493 crit. 1: automático usa o locale do navegador, não o idioma da UI', () => {
    languages(['pt-BR', 'en'])
    expect(resolvePrefs({}, 'en').locale).toBe('pt-BR')
    languages(['en-GB', 'en'])
    expect(resolvePrefs({}, 'pt-BR').locale).toBe('en-GB')
  })

  // F-0493 crit. 1
  it('F-0493 crit. 1: automático usa o fuso do computador', () => {
    expect(resolvePrefs({}, 'pt-BR').timeZone).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone)
    expect(detectTimeZone()).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone)
  })

  // F-0493 crit. 1
  it('F-0493 crit. 1: o valor guardado vale mais que o do navegador', () => {
    languages(['pt-BR'])
    expect(resolvePrefs({ locale: 'en-GB', timeZone: 'Europe/Lisbon' }, 'pt-BR')).toEqual({
      locale: 'en-GB',
      timeZone: 'Europe/Lisbon',
    })
  })

  // F-0493 crit. 5
  it('F-0493 crit. 5: sem locale válido no navegador cai no idioma da interface', () => {
    languages([])
    expect(resolvePrefs({}, 'pt-BR').locale).toBe('pt-BR')
    expect(detectLocale('en')).toBe('en')
    languages(['xx-INVALID', 'não é locale'])
    expect(resolvePrefs({}, 'en').locale).toBe('en')
  })

  // F-0493 crit. 5
  it('F-0493 crit. 5: o primeiro locale válido de navigator.languages vale', () => {
    languages(['xx-INVALID', 'es-MX', 'en'])
    expect(detectLocale('pt-BR')).toBe('es-MX')
  })

  // F-0493 crit. 5
  it('F-0493 crit. 5: sem fuso do navegador não há fuso resolvido, e formatar não lança', () => {
    vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions').mockReturnValue({
      locale: 'pt-BR',
    } as Intl.ResolvedDateTimeFormatOptions)
    const p = resolvePrefs({}, 'pt-BR')
    expect(p.timeZone).toBeUndefined()
    expect(formatInstant(ISO, 'full', p)).not.toBe('')
  })
})

describe('guarda no navegador (F-0493)', () => {
  // F-0493 crit. 8
  it('F-0493 crit. 8: lê o que foi guardado, nas chaves ao lado do cerne-lang', () => {
    expect(LOCALE_STORE).toBe(DATE_LOCALE_KEY)
    expect(ZONE_STORE).toBe(TIME_ZONE_KEY)
    localStorage.setItem(LOCALE_STORE, 'en-GB')
    localStorage.setItem(ZONE_STORE, 'Europe/Lisbon')
    expect(readStored()).toEqual({ locale: 'en-GB', timeZone: 'Europe/Lisbon' })
  })

  // F-0493 crit. 8
  it('F-0493 crit. 8: gravar um valor guarda, e gravar undefined apaga a chave', () => {
    expect(writeStored(LOCALE_STORE, 'en-GB')).toBe(true)
    expect(localStorage.getItem(LOCALE_STORE)).toBe('en-GB')
    expect(writeStored(LOCALE_STORE, undefined)).toBe(true)
    expect(localStorage.getItem(LOCALE_STORE)).toBeNull()
  })

  // F-0493 crit. 9
  it('F-0493 crit. 9: valor guardado inválido é ignorado', () => {
    localStorage.setItem(LOCALE_STORE, 'xx-INVALID')
    localStorage.setItem(ZONE_STORE, 'Lua/Base')
    expect(readStored()).toEqual({})
  })

  // F-0493 crit. 9
  it('F-0493 crit. 9: localStorage que lança na leitura dá o automático', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new DOMException('negado', 'SecurityError')
    })
    expect(readStored()).toEqual({})
  })

  // F-0493 crit. 9
  it('F-0493 crit. 9: localStorage que lança na gravação devolve false, sem lançar', () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new DOMException('cheio', 'QuotaExceededError')
    })
    vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(() => {
      throw new DOMException('negado', 'SecurityError')
    })
    expect(writeStored(LOCALE_STORE, 'en-GB')).toBe(false)
    expect(writeStored(LOCALE_STORE, undefined)).toBe(false)
  })
})
