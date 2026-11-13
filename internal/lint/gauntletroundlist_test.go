package lint

import (
	"slices"
	"testing"
)

// F-0391 crit. 5: the costs detail lists each Gauntlet round with its outcome,
// read by the same parser as GauntletRounds — so the list never contradicts
// the defeats the table shows.
func TestGauntletRoundList(t *testing.T) {
	cases := []struct {
		name, result string
		want         []RoundOutcome
	}{
		{"rounds in number order, normalized", "## Gauntlet\n\nBarra: x\nRodada 2 — vitória: \"b\"\nRodada 1 — derrota: \"a\"\nRodada 3 — empate: \"c\"\n",
			[]RoundOutcome{{1, "loss"}, {2, "win"}, {3, "tie"}}},
		{"English", "## Gauntlet\n\nBar: x\nRound 1 — loss: \"a\"\nRound 2 — win: \"b\"\n",
			[]RoundOutcome{{1, "loss"}, {2, "win"}}},
		{"same round twice keeps the worse", "## Gauntlet\n\nBarra: x\nRodada 1 — vitória: \"a\"\nRodada 1 — derrota: \"b\"\n",
			[]RoundOutcome{{1, "loss"}}},
		{"implicit round", "## Gauntlet\n\nBarra: x\nVeredito: derrota — \"a\"\n", []RoundOutcome{{0, "loss"}}},
		{"waiver has no round", "## Gauntlet\n\nBarra: dispensada por decisão humana (D-0001)\n", nil},
		{"only the bar", "## Gauntlet\n\nBarra: x\n", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := GauntletRoundList([]byte(c.result))
			if !slices.Equal(got, c.want) {
				t.Errorf("GauntletRoundList = %v, want %v", got, c.want)
			}
			_, defeats := GauntletRounds([]byte(c.result))
			losses := 0
			for _, r := range got {
				if r.Outcome == "loss" {
					losses++
				}
			}
			if losses != defeats {
				t.Errorf("list has %d losses, GauntletRounds counts %d", losses, defeats)
			}
		})
	}
}
