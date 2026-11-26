import { afterEach, describe, expect, it } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Markdown from './Markdown'
import { I18nProvider } from './i18n'
import { parseClarification } from './clarify'

afterEach(() => {
  cleanup()
  localStorage.removeItem('cerne-lang')
})

function open(md: string, lang: 'pt-BR' | 'en') {
  localStorage.setItem('cerne-lang', lang)
  const answers: string[] = []
  render(
    <I18nProvider>
      <Markdown onAnswer={(_raw, _offset, answer) => answers.push(answer)}>{md}</Markdown>
    </I18nProvider>,
  )
  fireEvent.click(screen.getAllByRole('button')[0])
  return answers
}

// Os rótulos dos botões do popover, na ordem em que aparecem (sem o chip).
const labels = () =>
  screen
    .getAllByRole('button')
    .slice(1)
    .map((b) => b.textContent)

describe('F-0517: esclarecimento com opções e recomendada', () => {
  // F-0517 crit. 1
  it('o leitor devolve a opção sem o sufixo e a posição da recomendada', () => {
    const info = parseClarification('[ESCLARECER: q? | a | b (recomendada)]')
    expect(info?.options).toEqual(['a', 'b'])
    expect(info?.recommended).toBe(2)
    expect(parseClarification('[CLARIFY: q? | a (Recommended) | b]')?.recommended).toBe(1)
    expect(parseClarification('[CLARIFY: q? | a (recommended) x | b]')?.recommended).toBe(0)
    expect(parseClarification('[CLARIFY: q? | a (recommended) | b (recommended)]')?.options).toEqual(['a', 'b'])
  })

  // F-0517 crit. 4
  it('pt-BR: recomendada primeiro com o selo, as outras na ordem escrita, depois «Outra resposta»', () => {
    const answers = open('[ESCLARECER: q? | a | b (recomendada) | c]', 'pt-BR')
    expect(labels().slice(0, 4)).toEqual(['bRecomendada', 'a', 'c', 'Outra resposta'])
    expect(screen.queryByRole('textbox')).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: /^b/ }))
    // F-0517 crit. 3: a resposta gravada não leva o sufixo.
    expect(answers).toEqual(['b'])
  })

  // F-0517 crit. 4
  it('en: recommended first with the badge, then «Other answer», which opens the text box', () => {
    const answers = open('[CLARIFY: q? | a | b (recommended)]', 'en')
    expect(labels().slice(0, 3)).toEqual(['bRecommended', 'a', 'Other answer'])
    fireEvent.click(screen.getByRole('button', { name: 'Other answer' }))
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'free' } })
    fireEvent.click(screen.getByRole('button', { name: 'Answer' }))
    expect(answers).toEqual(['free'])
  })

  // F-0517 crit. 4
  it('sem recomendada: a ordem escrita, sem selo, e a entrada «Outra resposta»', () => {
    open('[ESCLARECER: q? | a | b | c]', 'pt-BR')
    expect(labels().slice(0, 4)).toEqual(['a', 'b', 'c', 'Outra resposta'])
  })

  // F-0517 crit. 4
  it('sem opções: só a caixa de texto, como hoje', () => {
    open('[ESCLARECER: q?]', 'pt-BR')
    expect(screen.getByRole('textbox')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Outra resposta' })).toBeNull()
  })

  // F-0517 crit. 1
  it('um sufixo repetido sai inteiro', () => {
    const info = parseClarification('[CLARIFY: q? | a | b (recommended) (recomendada)]')
    expect(info?.options).toEqual(['a', 'b'])
    expect(info?.recommended).toBe(2)
    expect(parseClarification('[CLARIFY: q? | (recommended) | b]')?.options).toEqual(['(recommended)', 'b'])
  })

  // F-0517 crit. 4
  it('o nome acessível separa a opção do selo', () => {
    open('[CLARIFY: q? | a | b (recommended)]', 'en')
    expect(screen.getByRole('button', { name: 'b, Recommended' })).toBeInTheDocument()
  })

  // F-0517 crit. 4
  it('cancelar fecha a caixa de texto: ao reabrir, volta à lista de opções', () => {
    open('[CLARIFY: q? | a | b]', 'en')
    fireEvent.click(screen.getByRole('button', { name: 'Other answer' }))
    expect(screen.getByRole('textbox')).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    fireEvent.click(screen.getAllByRole('button')[0])
    expect(screen.queryByRole('textbox')).toBeNull()
    expect(screen.getByRole('button', { name: 'Other answer' })).toBeInTheDocument()
  })
})
