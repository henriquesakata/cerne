package server

import "io"

// The panel's Update button (F-0480) swaps the binary with cerne update's own
// code (F-0476), which lives in the CLI package this one cannot import: the
// CLI injects both functions, like CommandTree.
var (
	// PlanBinary says, without the network, whether exe can be replaced from
	// inside; when it cannot, why and the command the person runs instead.
	PlanBinary func(exe string) (swappable bool, why, fix string)
	// ReplaceBinary brings exe to the latest release the way it was installed,
	// writing progress to w.
	ReplaceBinary func(w io.Writer, exe string) BinaryOutcome
)

// BinaryOutcome is the end of one swap: State is updated, up to date or not
// done; Fix is the command to run when not done; NewExe the executable that
// changed, when one did.
type BinaryOutcome struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
	Fix    string `json:"fix,omitempty"`
	NewExe string `json:"new_exe,omitempty"`
}
