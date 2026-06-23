package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"gitlab.com/rockt/cerne/internal/app"
	"gitlab.com/rockt/cerne/internal/domain"
	"gitlab.com/rockt/cerne/internal/store"
)

func newNoteCmd() *cobra.Command {
	var dir string
	cmd := &cobra.Command{
		Use:   "note",
		Short: "Create and manage notes (durable team knowledge) in the backend",
	}
	cmd.PersistentFlags().StringVar(&dir, "dir", "", "project directory (defaults to cwd)")
	_ = cmd.PersistentFlags().MarkHidden("dir")
	cmd.AddCommand(newNoteNewCmd(&dir))
	cmd.AddCommand(newNoteListCmd(&dir))
	cmd.AddCommand(newNoteShowCmd(&dir))
	cmd.AddCommand(newNoteKeepCmd(&dir))
	cmd.AddCommand(newNoteRetireCmd(&dir))
	cmd.AddCommand(newNoteSubstituirCmd(&dir))
	return cmd
}

func newNoteListCmd(dir *string) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Listar notas (conhecimento durável) — ativas por padrão; --all inclui história",
		Long: "Lista as notas lidas direto do backend (store), não da projeção '.docs/' — então enxerga o " +
			"conhecimento mesmo quando '.docs/' está desatualizado (sem 'cerne pull'). Por padrão mostra só as " +
			"ativas; --all inclui as superseded (história). Cada linha traz id, estado, título e marcadores " +
			"(durável, informa F-/D-, sucessora).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteList(cmd, *dir, all)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "incluir notas superseded (história)")
	return cmd
}

func runNoteList(cmd *cobra.Command, dir string, all bool) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	notes, err := store.LoadAllNotes(ctx.repoDir)
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	shown := 0
	for _, n := range notes {
		if !all && n.State != domain.NoteActive {
			continue
		}
		shown++
		line := fmt.Sprintf("%s  [%s]  %s", n.ID, n.State, n.Title)
		var tags []string
		if n.Durable {
			tags = append(tags, "durável")
		}
		if len(n.Informs) > 0 {
			tags = append(tags, "informa "+strings.Join(n.Informs, ","))
		}
		if n.SupersededBy != "" {
			tags = append(tags, "→ "+n.SupersededBy)
		}
		if len(tags) > 0 {
			line += "  · " + strings.Join(tags, " · ")
		}
		fmt.Fprintln(w, line)
	}
	if shown == 0 {
		if all {
			fmt.Fprintln(w, "Nenhuma nota registrada. Use 'cerne note new \"<título>\"' para registrar conhecimento durável.")
		} else {
			fmt.Fprintln(w, "Nenhuma nota ativa. Use 'cerne note new \"<título>\"' para registrar, ou 'cerne note list --all' para ver história.")
		}
	}
	return nil
}

func newNoteShowCmd(dir *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Mostrar uma nota (metadados + corpo) lida direto do backend",
		Long: "Imprime os metadados (id, estado, durável, informa, motivo/sucessora) e o corpo markdown da nota, " +
			"ambos lidos direto do backend (store) — imune a uma projeção '.docs/' desatualizada.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteShow(cmd, *dir, args[0])
		},
	}
	return cmd
}

func runNoteShow(cmd *cobra.Command, dir, id string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	n, err := store.LoadNote(ctx.repoDir, id)
	if err != nil {
		return fmt.Errorf("nota %s: %w", id, err)
	}
	w := cmd.OutOrStdout()
	fmt.Fprintf(w, "%s  [%s]  %s\n", n.ID, n.State, n.Title)
	if n.Durable {
		fmt.Fprintln(w, "durável: sim")
	}
	if len(n.Informs) > 0 {
		fmt.Fprintf(w, "informa: %s\n", strings.Join(n.Informs, ", "))
	}
	if n.SupersededBy != "" {
		fmt.Fprintf(w, "substituída por: %s\n", n.SupersededBy)
	}
	if n.Reason != "" {
		fmt.Fprintf(w, "motivo: %s\n", n.Reason)
	}
	body, err := store.LoadNoteContent(ctx.repoDir, id)
	if err != nil {
		return fmt.Errorf("corpo da nota %s: %w", id, err)
	}
	fmt.Fprintf(w, "\n%s\n", strings.TrimRight(body, "\n"))
	return nil
}

func newNoteNewCmd(dir *string) *cobra.Command {
	var informs, file string
	cmd := &cobra.Command{
		Use:   "new <title>",
		Short: "Register durable team knowledge as a note (active)",
		Long: "Registra conhecimento durável de projeto (gotchas, invariantes, convenções tácitas) como uma nota " +
			"ativa. O corpo é texto livre, vindo de --file ou da entrada padrão (quando há pipe); sem corpo a nota " +
			"nasce como stub para você preencher. Use --informs para ligar a nota às features/decisões (F-/D-) que " +
			"ela informa. Conhecimento pessoal/de-máquina (paths, preferências) NÃO vai aqui — fica na memória local.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteNew(cmd, *dir, args[0], informs, file)
		},
	}
	cmd.Flags().StringVar(&informs, "informs", "", "comma-separated F-/D- ids this note informs")
	cmd.Flags().StringVar(&file, "file", "", "file with the note body (defaults to piped stdin)")
	return cmd
}

func runNoteNew(cmd *cobra.Command, dir, title, informs, file string) error {
	body, err := readNoteBody(cmd, file)
	if err != nil {
		return err
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	n, err := app.CreateNote(ctx.repoDir, ctx.projDir, title, body, splitInforms(informs))
	if err != nil {
		return err
	}
	// A note created with a body is already real content, committed and pushed
	// by CreateNote (CommitAndSnapshot) — there is nothing to fill or push.
	// Confirm it is synced and point at 'cerne pull', so the caller doesn't
	// pull+inspect just to verify a complete operation. A bodyless note is a
	// stub: keep the fill + push next step.
	if strings.TrimSpace(body) != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "✓ %s criada (active) com corpo — já sincronizada. Rode 'cerne pull' para materializar localmente.\n", n.ID)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s criada (active). Próximo passo: preencha a nota em '.docs/current/notes/%s-<slug>.md' e rode 'cerne push'.\n", n.ID, n.ID)
	return nil
}

// readNoteBody reads the note body from --file when given, or from stdin only
// when it is piped (not an interactive terminal). A note with no body is valid:
// it is born a stub. This keeps 'cerne note new' from blocking on stdin when no
// body is supplied.
func readNoteBody(cmd *cobra.Command, file string) (string, error) {
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", fmt.Errorf("--file: %w", err)
		}
		return string(data), nil
	}
	if !stdinPiped() {
		return "", nil
	}
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// stdinPiped reports whether os.Stdin is a pipe/redirect rather than an
// interactive terminal, so optional-body commands read it only when content was
// actually piped in.
func stdinPiped() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

func newNoteRetireCmd(dir *string) *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "retire <id>",
		Short: "Aposentar uma nota obsoleta (vai para superseded, sem sucessora; --reason obrigatório)",
		Long: "Move uma nota de active para superseded SEM sucessora — para conhecimento que não vale mais " +
			"(ex.: a feature que a nota informava já entregou). A nota sai do estado atual (.docs/CONTEXT.md) e " +
			"vira história pesquisável ('cerne search'/'ask'). Para trocar por uma versão corrigida, use " +
			"'cerne note substituir --by'. O 'cerne lint' sinaliza (stale-note) notas cujo --informs já encerrou.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteRetire(cmd, *dir, args[0], reason)
		},
	}
	cmd.Flags().StringVar(&reason, "reason", "", "motivo da aposentadoria (obrigatório)")
	return cmd
}

func runNoteRetire(cmd *cobra.Command, dir, id, reason string) error {
	// Validate the required reason before resolveProject so bad invocations
	// fail fast without touching origin. retire is substituir with no successor.
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("--reason é obrigatório")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	out, err := app.TransitionNote(ctx.repoDir, ctx.projDir, id, "", reason)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s aposentada: %s → %s\n", out.ID, out.From, out.To)
	return nil
}

func newNoteKeepCmd(dir *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keep <id>",
		Short: "Marcar uma nota como durável (o lint para de sinalizá-la como stale-note)",
		Long: "Marca uma nota ativa como conhecimento durável: o veredito humano de que ela vale, mesmo que " +
			"informe (--informs) uma feature/decisão já encerrada. A nota continua active e materializada; o " +
			"'cerne lint' deixa de sinalizá-la como stale-note. Idempotente. Para aposentar de vez, use 'cerne note retire'.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteKeep(cmd, *dir, args[0])
		},
	}
	return cmd
}

func runNoteKeep(cmd *cobra.Command, dir, id string) error {
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	n, err := app.KeepNote(ctx.repoDir, ctx.projDir, id)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s marcada como durável — o 'cerne lint' não a sinaliza mais (stale-note).\n", n.ID)
	return nil
}

func newNoteSubstituirCmd(dir *string) *cobra.Command {
	var by, reason string
	cmd := &cobra.Command{
		Use:   "substituir <id>",
		Short: "Substituir ou retirar uma nota (--by opcional; --reason obrigatório)",
		Long: "Move uma nota de active para superseded. Com --by, a nota sucessora a substitui (o conhecimento " +
			"corrigido materializa no lugar); sem --by, a nota é apenas retirada. A nota antiga vira história " +
			"pesquisável ('cerne search'/'ask').",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNoteSubstituir(cmd, *dir, args[0], by, reason)
		},
	}
	cmd.Flags().StringVar(&by, "by", "", "id da nota que substitui (opcional; vazio = apenas retira)")
	cmd.Flags().StringVar(&reason, "reason", "", "motivo da substituição/retirada (obrigatório)")
	return cmd
}

func runNoteSubstituir(cmd *cobra.Command, dir, id, by, reason string) error {
	// Validate the required reason before resolveProject so bad invocations
	// fail fast without touching origin.
	if strings.TrimSpace(reason) == "" {
		return fmt.Errorf("--reason é obrigatório")
	}
	ctx, err := resolveProject(dir)
	if err != nil {
		return err
	}
	out, err := app.TransitionNote(ctx.repoDir, ctx.projDir, id, by, reason)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", out.ID, out.From, out.To)
	return nil
}
