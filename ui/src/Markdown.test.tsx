import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import '@testing-library/jest-dom/vitest'
import Markdown from './Markdown'
import { markerSpans, resolveMarker } from './clarify'

const BLOCO = ['Antes do bloco.', '', '```js', 'const a = 1', 'console.log(a)', '```', '', 'Depois.'].join('\n')

const writeText = vi.fn<(text: string) => Promise<void>>()

function withClipboard(impl: ((text: string) => Promise<void>) | null) {
  Object.defineProperty(navigator, 'clipboard', {
    value: impl ? { writeText: (t: string) => writeText(t) } : undefined,
    configurable: true,
  })
  if (impl) writeText.mockImplementation(impl)
}

beforeEach(() => {
  writeText.mockReset()
  withClipboard(() => Promise.resolve())
})

afterEach(cleanup)

describe('Markdown (F-0247): botão de copiar no bloco cercado', () => {
  it('critérios 1 e 5: o bloco ganha um botão com nome acessível', () => {
    render(<Markdown>{BLOCO}</Markdown>)
    expect(screen.getByRole('button', { name: /copiar bloco de código/i })).toBeInTheDocument()
  })

  it('critério 2: copia exatamente o conteúdo do bloco, sem cerca nem rótulo de linguagem', () => {
    render(<Markdown>{BLOCO}</Markdown>)
    fireEvent.click(screen.getByRole('button', { name: /copiar bloco de código/i }))
    expect(writeText).toHaveBeenCalledWith('const a = 1\nconsole.log(a)\n')
  })

  it('critério 3: confirma a cópia e volta sozinho ao estado anterior', async () => {
    vi.useFakeTimers()
    try {
      render(<Markdown>{BLOCO}</Markdown>)
      const botao = screen.getByRole('button', { name: /copiar bloco de código/i })
      fireEvent.click(botao)
      await act(async () => {})
      expect(screen.getByText('Copiado!')).toBeInTheDocument()

      await act(async () => {
        vi.advanceTimersByTime(2000)
      })
      expect(screen.queryByText('Copiado!')).not.toBeInTheDocument()
      expect(screen.getByRole('button', { name: /copiar bloco de código/i })).toBeInTheDocument()
    } finally {
      vi.useRealTimers()
    }
  })

  it('critério 6: sem clipboard disponível não lança nem afirma sucesso', () => {
    withClipboard(null)
    render(<Markdown>{BLOCO}</Markdown>)
    expect(() => fireEvent.click(screen.getByRole('button', { name: /copiar bloco de código/i }))).not.toThrow()
    expect(screen.queryByText('Copiado!')).not.toBeInTheDocument()
  })

  it('critério 6: writeText rejeitando não lança nem afirma sucesso', async () => {
    withClipboard(() => Promise.reject(new Error('permissão negada')))
    render(<Markdown>{BLOCO}</Markdown>)
    fireEvent.click(screen.getByRole('button', { name: /copiar bloco de código/i }))
    await act(async () => {})
    expect(screen.queryByText('Copiado!')).not.toBeInTheDocument()
  })

  it('não acrescenta botão a código inline', () => {
    render(<Markdown>{'Um `trecho` inline.'}</Markdown>)
    expect(screen.queryByRole('button')).not.toBeInTheDocument()
  })
})

// F-0282: o marcador dentro de célula de tabela. O pipe cru é separador de
// CÉLULA, então o marcador com opções tem de ser escrito com `\|` — a forma
// escapada mantém a célula inteira e separa as opções do mesmo jeito.
const tabela = (celula: string) =>
  ['| Fork | Razão |', '| --- | --- |', `| ${celula} | porque sim |`].join('\n')

describe('Markdown (F-0282): chip de esclarecimento em célula de tabela', () => {
  it('critério 1: marcador sem opções na célula vira chip respondível', () => {
    render(<Markdown onAnswer={() => {}}>{tabela('[ESCLARECER: qual formato?]')}</Markdown>)
    expect(screen.getByRole('button', { name: /qual formato\?/ })).toBeInTheDocument()
  })

  it('critério 1: marcador com opções escapadas na célula vira chip com as opções no popover', () => {
    render(
      <Markdown onAnswer={() => {}}>{tabela('[ESCLARECER: qual formato? \\| tabela \\| lista]')}</Markdown>,
    )
    const chip = screen.getByRole('button', { name: /qual formato\?/ })
    expect(chip).toBeInTheDocument()
    // O rótulo é a pergunta já sem as barras invertidas do escape.
    expect(chip).toHaveTextContent('? qual formato?')
    fireEvent.click(chip)
    expect(screen.getByRole('button', { name: 'tabela' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'lista' })).toBeInTheDocument()
  })

  it('critério 1: marcador resolvido na célula vira chip verde com a trilha', () => {
    const { container } = render(<Markdown>{tabela('[ESCLARECIDO: qual formato? → tabela]')}</Markdown>)
    expect(container.querySelector('.clarify-resolved')).toHaveTextContent('qual formato? → tabela')
  })

  it('critério 2: todo marcador contado pelo badge é um chip renderizado, dentro da tabela', () => {
    const md = [
      tabela('[ESCLARECER: com opções? \\| a \\| b]'),
      '',
      // O pipe CRU na célula é o caso que quebrava: o parser GFM fatia o
      // marcador em várias células antes de qualquer lógica de chip. Contado
      // pelo badge, tem de virar chip também — senão a paridade é só do
      // caminho já conforme.
      tabela('[ESCLARECER: pipe cru? | a | b]'),
      '',
      'Em prosa: [ESCLARECER: sem tabela? | a | b] e [ESCLARECIDO: outra? → ok].',
    ].join('\n')
    const { container } = render(<Markdown onAnswer={() => {}}>{md}</Markdown>)
    expect(container.querySelectorAll('.clarify-chip')).toHaveLength(markerSpans(md).length)
    // O chip resolvido também carrega .clarify-chip — sem isto a contagem acima
    // passaria por fora de um marcador inteiro.
    expect(container.querySelectorAll('.clarify-resolved').length).toBeGreaterThan(0)
    // Nenhuma linha perdeu ou ganhou célula: as duas tabelas têm 2 colunas.
    for (const row of container.querySelectorAll('tbody tr')) {
      expect(row.querySelectorAll('td')).toHaveLength(2)
    }
  })

  it('critério 3: texto ao redor do marcador escapado sobrevive ao recorte', () => {
    // O nó de texto já vem DESESCAPADO do parser, mais curto que a fonte: a
    // sobra tem de ser recortada da fonte, senão as barras invertidas do escape
    // comem o texto seguinte (` final` virava `inal`).
    const md = tabela('custo [ESCLARECER: q? \\| a \\| b] final')
    const { container } = render(<Markdown onAnswer={() => {}}>{md}</Markdown>)
    const celulas = Array.from(container.querySelectorAll('tbody td')).map((t) => t.textContent)
    expect(celulas).toEqual(['custo ? q? final', 'porque sim'])
  })

  it('limite conhecido: com pipe CRU, o texto vizinho da célula é absorvido pelo chip', () => {
    // Aqui as bordas do span são células inteiras, não nós de texto — a regra de
    // borda da F-0181 (nó não-texto na borda é absorvido) vale igual em tabela.
    // Pinado para que a perda seja uma decisão, não uma surpresa.
    const md = tabela('custo [ESCLARECER: q? | a | b] final')
    const { container } = render(<Markdown onAnswer={() => {}}>{md}</Markdown>)
    const celulas = Array.from(container.querySelectorAll('tbody td')).map((t) => t.textContent)
    expect(celulas).toEqual(['? q?', 'porque sim'])
  })

  it('marcador não atravessa linhas, então o dono nunca é a própria tabela', () => {
    // O corpo do marcador proíbe quebra de linha (CLARIFY_RE), e uma linha de
    // tabela é uma linha de texto: um span não pode cobrir duas tableRow. É o
    // que garante que reembrulhar só tableRow basta.
    const md = ['| A | B |', '| --- | --- |', '| [ESCLARECER: um? | x] | y |', '| z | [ESCLARECER: dois? | w] |'].join('\n')
    const { container } = render(<Markdown onAnswer={() => {}}>{md}</Markdown>)
    expect(container.querySelectorAll('.clarify-chip')).toHaveLength(markerSpans(md).length)
    for (const row of container.querySelectorAll('tbody tr')) {
      expect(row.querySelectorAll('td')).toHaveLength(2)
    }
  })

  it('critério 1 e 2: marcador com pipe CRU na célula ainda vira chip, dentro de um <td>', () => {
    const md = tabela('[ESCLARECER: pipe cru? | a | b]')
    const { container } = render(<Markdown onAnswer={() => {}}>{md}</Markdown>)
    const chip = screen.getByRole('button', { name: /pipe cru\?/ })
    expect(chip).toBeInTheDocument()
    // O chip vive dentro de uma célula — linha de tabela válida, não filho solto.
    expect(chip.closest('td')).not.toBeNull()
    // O popover ainda oferece as opções que o marcador propôs.
    fireEvent.click(chip)
    expect(screen.getByRole('button', { name: 'a' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'b' })).toBeInTheDocument()
  })

  it('critério 3: responder um marcador com pipe cru devolve a linha à forma válida', () => {
    const md = tabela('[ESCLARECER: pipe cru? | a | b]')
    const respostas: Array<[string, number, string]> = []
    const { unmount } = render(
      <Markdown onAnswer={(raw, offset, answer) => respostas.push([raw, offset, answer])}>{md}</Markdown>,
    )
    fireEvent.click(screen.getByRole('button', { name: /pipe cru\?/ }))
    fireEvent.click(screen.getByRole('button', { name: 'a' }))
    unmount()

    const [raw, offset, answer] = respostas[0]
    const proximo = resolveMarker(md, raw, offset, answer) as string
    const depois = render(<Markdown>{proximo}</Markdown>)
    // A forma resolvida não tem pipe, então a linha volta a ter 2 células.
    expect(depois.container.querySelectorAll('td')).toHaveLength(2)
    expect(depois.container.querySelector('.clarify-resolved')).toHaveTextContent('pipe cru? → a')
  })

  it('critério 3: responder pelo chip reescreve a fonte sem quebrar a tabela', () => {
    const md = tabela('[ESCLARECER: qual formato? \\| tabela \\| lista]')
    const respostas: Array<[string, number, string]> = []
    const { container, unmount } = render(
      <Markdown onAnswer={(raw, offset, answer) => respostas.push([raw, offset, answer])}>{md}</Markdown>,
    )
    const celulasAntes = container.querySelectorAll('td').length
    fireEvent.click(screen.getByRole('button', { name: /qual formato\?/ }))
    fireEvent.click(screen.getByRole('button', { name: 'tabela' }))
    unmount()

    expect(respostas).toHaveLength(1)
    const [raw, offset, answer] = respostas[0]
    const proximo = resolveMarker(md, raw, offset, answer)
    expect(proximo).not.toBeNull()

    const depois = render(<Markdown>{proximo as string}</Markdown>)
    expect(depois.container.querySelectorAll('td')).toHaveLength(celulasAntes)
    expect(depois.container.querySelector('.clarify-resolved')).toHaveTextContent(
      'qual formato? → tabela',
    )
  })

  it('critério 3: resposta livre com pipe não vira separador de célula', () => {
    const md = tabela('[ESCLARECER: qual formato?]')
    const span = markerSpans(md)[0]
    const proximo = resolveMarker(md, span.raw, span.offset, 'tabela | lista') as string
    expect(proximo).toContain('[ESCLARECIDO: qual formato? → tabela \\| lista]')

    const { container } = render(<Markdown>{proximo}</Markdown>)
    expect(container.querySelectorAll('td')).toHaveLength(2)
    expect(container.querySelector('.clarify-resolved')).toHaveTextContent(
      'qual formato? → tabela | lista',
    )
  })
})
