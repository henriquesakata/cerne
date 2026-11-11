package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/henriquesakata/cerne/internal/llm"
)

// RoastPersona is one adversarial critic lens (F-0271). Three fixed personas
// by design: a configurable set was cut as speculation, and identical critics
// would be redundancy, not diversity.
type RoastPersona struct {
	Key   string // stable identifier ("ambiguidade", "escopo", "risco")
	Title string // pt-BR heading shown in the report
	Lens  string // the single attack angle, verbatim in the prompt
	// Matrix is an exhaustive block the persona must emit before its
	// findings, outside the five-finding cap (F-0275): a per-criterion
	// coverage verdict is a matrix, not a finding, and capping it would make
	// the contract unsatisfiable on a spec with six criteria.
	Matrix string
}

// RoastPersonas are the fixed critics of the spec roast, in report order.
var RoastPersonas = []RoastPersona{
	{
		Key:   "ambiguidade",
		Title: "Ambiguidade e testabilidade",
		Lens:  "AMBIGUIDADE E TESTABILIDADE: critérios não-verificáveis, termos vagos sem definição operacional, QUANDO/DEVE mal formados, condições sem observável mensurável, verificação que não cobre os critérios.",
	},
	{
		Key:   "escopo",
		Title: "Escopo e YAGNI",
		Lens:  "ESCOPO E YAGNI: escopo inflado, requisito especulativo sem caso de uso, fora-de-escopo furado (o que vai vazar pra dentro), feature que deveria ser menor ou maior, sobreposição interna na própria spec.",
	},
	{
		Key:   "risco",
		Title: "Risco técnico e viabilidade",
		Lens:  "RISCO TÉCNICO E VIABILIDADE: dependências escondidas, efeitos colaterais no sistema existente, custo/latência de runtime, suposições técnicas não validadas, plano de verificação tecnicamente irrealizável.",
	},
}

// PlanRoastPersonas are the fixed critics of the plan roast (F-0275). The
// lenses are disjoint on purpose: verification coverage belongs to the
// conformity lens, so the risk critic never re-reports it as its own finding.
var PlanRoastPersonas = []RoastPersona{
	{
		Key:   "conformidade",
		Title: "Conformidade spec→plano",
		Lens:  "CONFORMIDADE SPEC→PLANO: cobertura de cada critério de aceitação numerado da spec pelo plano. Ataque: critério sem tarefa que o cubra, tarefa que não serve critério nenhum (escopo que o plano inventou), verificação declarada que não prova o critério que diz provar, e critério cuja cobertura depende de trabalho que o plano deixa implícito.",
		Matrix: "Antes dos achados, emita a MATRIZ DE COBERTURA: uma linha por critério numerado da spec, no formato exato \"critério N: coberto pela tarefa X\" ou \"critério N: descoberto\". A matriz é obrigatória, cobre TODOS os critérios (não tem teto e não conta como achado) e sai mesmo quando você não tiver nenhum achado.",
	},
	{
		Key:   "sequencia",
		Title: "Sequência e dependências",
		Lens:  "SEQUÊNCIA E DEPENDÊNCIAS: ordem que força retrabalho, dependência escondida entre tarefas (a tarefa N precisa de algo que só a N+2 cria), tarefa que não deixa o repositório verde ao terminar, paralelismo perdido (tarefas independentes serializadas sem razão) ou paralelismo falso (marcadas [paralela] mas tocam o mesmo arquivo), e granularidade errada — tarefa grande demais para um commit, ou fatiada a ponto de nenhuma entregar comportamento.",
	},
	{
		Key:   "rollback",
		Title: "Riscos e rollback",
		Lens:  "RISCOS E ROLLBACK: risco declarado sem forma de DETECTAR que aconteceu, rollback que não funciona na prática (migração de dados, artefato já publicado, estado externo mutado), passo destrutivo ou irreversível não sinalizado, e risco material que o plano não enxergou dado o que ele mesmo se propõe a mexer. Não avalie cobertura de critério da spec — essa lente é de outro crítico.",
	},
}

// RoastDoc is one delimited document handed to a critic. The label names the
// fence (=== SPEC (início) === …), so a multi-document roast keeps each source
// unambiguous instead of concatenating prose.
type RoastDoc struct {
	Label string // "SPEC", "PLANO"
	Body  string
}

// Roast intros: the one line that tells the critic what it is looking at.
// Kept separate from the personas so the same lens machinery serves both
// targets (F-0275).
const (
	SpecRoastIntro = "Você é um crítico adversarial de specs de software, contexto fresco: você recebeu APENAS a spec final abaixo — nada da conversa que a elaborou, nada do esforço gasto."
	PlanRoastIntro = "Você é um crítico adversarial de planos de implementação, contexto fresco: você recebeu APENAS a spec aprovada e o plano abaixo — nada da conversa que os elaborou, nada do esforço gasto. O plano se julga CONTRA a spec: a spec é o contrato, o plano é a promessa de como cumpri-lo."
)

// BuildRoastPrompt renders one critic's prompt over a single spec (F-0271).
func BuildRoastPrompt(p RoastPersona, featureID, title, spec string) string {
	return BuildRoastPromptFor(p, featureID, title, SpecRoastIntro, []RoastDoc{{Label: "SPEC", Body: spec}})
}

// BuildRoastPromptFor renders one critic's prompt over any document bundle.
// The contract mirrors what the F-0270/W-0181 dry runs proved out: exact quote
// or the finding is invalid, no verdict, hard cap, and every document
// explicitly delimited so instructions never bleed into the judged artifact.
func BuildRoastPromptFor(p RoastPersona, featureID, title, intro string, docs []RoastDoc) string {
	var b strings.Builder
	b.WriteString(intro + "\n\n")
	b.WriteString("SUA LENTE (única — ignore problemas de outras lentes): " + p.Lens + "\n\n")
	b.WriteString("Regras do contrato:\n")
	if len(docs) > 1 {
		// With more than one document in the prompt, an unsourced quote is
		// ambiguous — and the staleness stamp cannot tell which document the
		// finding is anchored to.
		b.WriteString("- Cada achado cita o trecho EXATO entre aspas E o documento de onde veio; achado sem trecho citado, ou sem o documento, é inválido e será descartado.\n")
	} else {
		b.WriteString("- Cada achado cita o trecho EXATO entre aspas; achado sem trecho citado é inválido e será descartado.\n")
	}
	b.WriteString("- Você nunca aprove nem reprove — a decisão é do gate humano; você só entrega achados.\n")
	b.WriteString("- Sem elogios, sem resumo; no máximo 5 achados, ordenados por severidade [alta|média|baixa].\n")
	if p.Matrix == "" {
		b.WriteString("- Nada relevante pela sua lente → responda exatamente \"sem achados relevantes\".\n")
	} else {
		b.WriteString("- Sem achados além da matriz → escreva a matriz e depois exatamente \"sem achados relevantes\".\n")
	}
	if len(docs) > 1 {
		labels := make([]string, len(docs))
		for i, d := range docs {
			labels[i] = strings.ToUpper(d.Label)
		}
		b.WriteString("- Formato de cada linha: - [sev] achado em 1-2 frases — trecho (" + strings.Join(labels, "|") + "): \"…\"\n")
	} else {
		b.WriteString("- Formato de cada linha: - [sev] achado em 1-2 frases — trecho: \"…\"\n")
	}
	if p.Matrix != "" {
		b.WriteString("\n" + p.Matrix + "\n")
	}
	b.WriteString("\n")
	for i, d := range docs {
		if i == 0 {
			fmt.Fprintf(&b, "=== %s (início) ===\n(%s) %s\n\n%s\n=== %s (fim) ===\n\n", d.Label, featureID, title, d.Body, d.Label)
			continue
		}
		fmt.Fprintf(&b, "=== %s (início) ===\n%s\n=== %s (fim) ===\n\n", d.Label, d.Body, d.Label)
	}
	return b.String()
}

// RoastResult is one critic's outcome: output on success, Err on a failed or
// timed-out critic (the round never dies with it — F-0271 criterion 1).
type RoastResult struct {
	Persona RoastPersona
	Output  string
	Err     string
}

// RoastReport aggregates the round; Partial marks that at least one critic
// did not finish, visibly, instead of pretending full coverage. At and
// DocSHA stamp which text this round judged — a later edit of that document
// makes the staleness detectable instead of silent.
type RoastReport struct {
	FeatureID string
	Title     string
	Results   []RoastResult
	Partial   bool
	At        time.Time
	// Fingerprints carries one entry per document the round read, in prompt
	// order. A plan roast asserts plan-against-spec conformity, so editing
	// EITHER document invalidates the verdicts — stamping only the judged
	// document would call a report fresh after its spec moved on.
	Fingerprints []RoastFingerprint
}

// RoastFingerprint stamps one document the round was judged against.
type RoastFingerprint struct {
	Label string // "spec", "plano" — as it appears in the stamp line
	SHA   string // first 8 hex chars of sha256(body)
}

// AllFailed reports a round where no critic finished — nothing worth
// persisting (a cancelled request or a dead provider, not a judgment). An
// empty round counts as failed: zero critics is zero coverage.
func (r RoastReport) AllFailed() bool {
	if len(r.Results) == 0 {
		return true
	}
	for _, res := range r.Results {
		if res.Err == "" {
			return false
		}
	}
	return true
}

// FirstErr returns a reason for a failed round, safe on an empty one — the
// caller reports it to the user, so it must never be an index panic.
func (r RoastReport) FirstErr() string {
	for _, res := range r.Results {
		if res.Err != "" {
			return res.Err
		}
	}
	return "nenhum crítico foi executado"
}

// RunRoast runs the spec personas in parallel over the spec (F-0271).
func RunRoast(ctx context.Context, prov llm.Provider, featureID, title, spec string, perCritic time.Duration) RoastReport {
	return RunRoastWith(ctx, prov, RoastPersonas, featureID, title, SpecRoastIntro, []RoastDoc{{Label: "SPEC", Body: spec}}, perCritic)
}

// RunRoastWith runs any persona set in parallel over any document bundle,
// each critic under its own timeout. The provider implementations are
// stateless, so concurrent Complete calls are safe (cli exec / plain HTTP).
// Every document in the bundle is fingerprinted: a plan roast asserts
// plan-against-spec conformity, so editing either one invalidates the
// verdicts and the staleness check has to see both.
func RunRoastWith(ctx context.Context, prov llm.Provider, personas []RoastPersona, featureID, title, intro string, docs []RoastDoc, perCritic time.Duration) RoastReport {
	rep := RoastReport{
		FeatureID: featureID,
		Title:     title,
		Results:   make([]RoastResult, len(personas)),
		At:        time.Now().UTC(),
	}
	for _, d := range docs {
		rep.Fingerprints = append(rep.Fingerprints, RoastFingerprint{
			Label: strings.ToLower(d.Label),
			SHA:   fmt.Sprintf("%x", sha256.Sum256([]byte(d.Body)))[:8],
		})
	}
	var wg sync.WaitGroup
	for i, p := range personas {
		wg.Add(1)
		go func(i int, p RoastPersona) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, perCritic)
			defer cancel()
			out, err := prov.Complete(cctx, BuildRoastPromptFor(p, featureID, title, intro, docs))
			if err != nil {
				rep.Results[i] = RoastResult{Persona: p, Err: err.Error()}
				return
			}
			// An empty answer is a critic that did not run, not a critic with
			// nothing to say — the contract has a literal phrase for that.
			// Counting it as success would print an empty section and call
			// the round complete.
			if strings.TrimSpace(out) == "" {
				rep.Results[i] = RoastResult{Persona: p, Err: "crítico devolveu resposta vazia"}
				return
			}
			rep.Results[i] = RoastResult{Persona: p, Output: strings.TrimSpace(out)}
		}(i, p)
	}
	wg.Wait()
	for _, r := range rep.Results {
		if r.Err != "" {
			rep.Partial = true
		}
	}
	return rep
}

// roastStamp matches one labelled fingerprint in a persisted report's stamp
// line; the staleness badge (F-0275 criterion 4) reads them back, so the
// stamp has a consumer instead of being decoration.
var roastStamp = regexp.MustCompile(`([a-zà-ú]+) sha256:([0-9a-f]{8})`)

// ParseRoastSHA returns the fingerprint a persisted report stamped for the
// given document label ("spec", "plano"), or "" when the report carries no
// readable stamp for it.
func ParseRoastSHA(md, label string) string {
	for _, m := range roastStamp.FindAllStringSubmatch(md, -1) {
		if m[1] == label {
			return m[2]
		}
	}
	return ""
}

// FormatRoastReport renders the persisted spec-roast artifact (F-0271).
func FormatRoastReport(rep RoastReport) string {
	return formatRoast(rep, "Roast da spec",
		"Críticos adversariais de contexto fresco; o roast não aprova nem reprova — os achados acompanham o pedido de aprovação (F-0271).")
}

// FormatPlanRoastReport renders the persisted plan-roast artifact (F-0275):
// its own file, its own header, stamped with the plan's fingerprint so the
// page can mark it stale once the plan moves on.
func FormatPlanRoastReport(rep RoastReport) string {
	return formatRoast(rep, "Roast do plano",
		"Críticos adversariais de contexto fresco julgando o plano CONTRA a spec; o roast não aprova nem reprova — os achados informam a implementação (F-0275).")
}

// formatRoast renders the persisted markdown artifact: one section per
// persona, failures marked as partial rather than dropped.
func formatRoast(rep RoastReport, heading, preamble string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s — %s (%s)\n\n", heading, rep.Title, rep.FeatureID)
	b.WriteString(preamble + "\n\n")
	if !rep.At.IsZero() {
		fmt.Fprintf(&b, "Rodada: %s", rep.At.Format(time.RFC3339))
		for _, f := range rep.Fingerprints {
			fmt.Fprintf(&b, " · %s sha256:%s", f.Label, f.SHA)
		}
		b.WriteString(" — se algum destes documentos mudou depois disso, os trechos citados podem não existir mais.\n\n")
	}
	if rep.Partial {
		b.WriteString("> Rodada **parcial**: pelo menos um crítico não concluiu; os achados abaixo não cobrem todas as lentes.\n\n")
	}
	for _, r := range rep.Results {
		fmt.Fprintf(&b, "## %s\n\n", r.Persona.Title)
		if r.Err != "" {
			fmt.Fprintf(&b, "_(parcial — crítico não concluiu: %s)_\n\n", r.Err)
			continue
		}
		b.WriteString(r.Output + "\n\n")
	}
	return b.String()
}
