// lineDiff (F-0255 critério 3): diff textual por linha entre duas revisões da
// proposta. Specs são markdown — comparar por linha basta para o leitor ver o
// que mudou, e não exige nada do backend nem dependência nova.

export type DiffKind = 'same' | 'add' | 'del'
export type DiffLine = { kind: DiffKind; text: string }

// ponytail: LCS O(n×m) em tempo e memória. Uma spec tem dezenas de linhas, não
// milhares — se um dia o painel precisar diffar documentos grandes, trocar por
// Myers/patience.
export function lineDiff(before: string, after: string): DiffLine[] {
  const a = before.split('\n')
  const b = after.split('\n')
  const m = a.length
  const n = b.length

  // lcs[i][j] = tamanho da maior subsequência comum de a[i:] e b[j:].
  const lcs: number[][] = Array.from({ length: m + 1 }, () => new Array<number>(n + 1).fill(0))
  for (let i = m - 1; i >= 0; i--) {
    for (let j = n - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1])
    }
  }

  const out: DiffLine[] = []
  let i = 0
  let j = 0
  while (i < m && j < n) {
    if (a[i] === b[j]) {
      out.push({ kind: 'same', text: a[i] })
      i++
      j++
      continue
    }
    // Empate vai para a remoção: a linha antiga aparece antes da nova, que é
    // como se lê uma alteração ("era isto, virou aquilo").
    if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      out.push({ kind: 'del', text: a[i] })
      i++
    } else {
      out.push({ kind: 'add', text: b[j] })
      j++
    }
  }
  while (i < m) out.push({ kind: 'del', text: a[i++] })
  while (j < n) out.push({ kind: 'add', text: b[j++] })
  return out
}
