package app

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"

	"gitlab.com/rockt/cerne/internal/llm"
)

// RoastPersona is one adversarial critic lens (F-0271). Three fixed personas
// by design: a configurable set was cut as speculation, and identical critics
// would be redundancy, not diversity.
type RoastPersona struct {
	Key   string // stable identifier ("ambiguidade", "escopo", "risco")
	Title string // pt-BR heading shown in the report
	Lens  string // the single attack angle, verbatim in the prompt
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

// BuildRoastPrompt renders one critic's prompt. The contract mirrors what the
// F-0270/W-0181 dry runs proved out: exact quote or the finding is invalid, no
// verdict, hard cap, and the spec explicitly delimited so instructions never
// bleed into the judged artifact.
func BuildRoastPrompt(p RoastPersona, featureID, title, spec string) string {
	var b strings.Builder
	b.WriteString("Você é um crítico adversarial de specs de software, contexto fresco: você recebeu APENAS a spec final abaixo — nada da conversa que a elaborou, nada do esforço gasto.\n\n")
	b.WriteString("SUA LENTE (única — ignore problemas de outras lentes): " + p.Lens + "\n\n")
	b.WriteString("Regras do contrato:\n")
	b.WriteString("- Cada achado cita o trecho EXATO da spec entre aspas; achado sem trecho citado é inválido e será descartado.\n")
	b.WriteString("- Você nunca aprove nem reprove a spec — a decisão é do gate humano; você só entrega achados.\n")
	b.WriteString("- Sem elogios, sem resumo; no máximo 5 achados, ordenados por severidade [alta|média|baixa].\n")
	b.WriteString("- Nada relevante pela sua lente → responda exatamente \"sem achados relevantes\".\n")
	b.WriteString("- Formato de cada linha: - [sev] achado em 1-2 frases — trecho: \"…\"\n\n")
	fmt.Fprintf(&b, "=== SPEC (início) ===\n(%s) %s\n\n%s\n=== SPEC (fim) ===\n", featureID, title, spec)
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
// SpecSHA stamp which text this round judged — a later spec edit makes the
// staleness detectable instead of silent.
type RoastReport struct {
	FeatureID string
	Title     string
	Results   []RoastResult
	Partial   bool
	At        time.Time
	SpecSHA   string // first 8 hex chars of sha256(spec)
}

// AllFailed reports a round where no critic finished — nothing worth
// persisting (a cancelled request or a dead provider, not a judgment).
func (r RoastReport) AllFailed() bool {
	for _, res := range r.Results {
		if res.Err == "" {
			return false
		}
	}
	return true
}

// RunRoast runs the three personas in parallel, each under its own timeout.
// The provider implementations are stateless, so concurrent Complete calls
// are safe (cli exec / plain HTTP).
func RunRoast(ctx context.Context, prov llm.Provider, featureID, title, spec string, perCritic time.Duration) RoastReport {
	rep := RoastReport{
		FeatureID: featureID,
		Title:     title,
		Results:   make([]RoastResult, len(RoastPersonas)),
		At:        time.Now().UTC(),
		SpecSHA:   fmt.Sprintf("%x", sha256.Sum256([]byte(spec)))[:8],
	}
	var wg sync.WaitGroup
	for i, p := range RoastPersonas {
		wg.Add(1)
		go func(i int, p RoastPersona) {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, perCritic)
			defer cancel()
			out, err := prov.Complete(cctx, BuildRoastPrompt(p, featureID, title, spec))
			if err != nil {
				rep.Results[i] = RoastResult{Persona: p, Err: err.Error()}
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

// FormatRoastReport renders the persisted markdown artifact: one section per
// persona, failures marked as partial rather than dropped.
func FormatRoastReport(rep RoastReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Roast da spec — %s (%s)\n\n", rep.Title, rep.FeatureID)
	b.WriteString("Críticos adversariais de contexto fresco; o roast não aprova nem reprova — os achados acompanham o pedido de aprovação (F-0271).\n\n")
	if !rep.At.IsZero() {
		fmt.Fprintf(&b, "Rodada: %s · spec sha256:%s — se a spec mudou depois disso, os trechos citados podem não existir mais.\n\n", rep.At.Format(time.RFC3339), rep.SpecSHA)
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
