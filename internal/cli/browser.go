package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// browserCommand returns the OS command (name + args) that opens url in the
// default browser. It is split out from openBrowser so it can be unit-tested
// per GOOS without actually launching anything.
func browserCommand(goos, url string) (string, []string) {
	switch goos {
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		return "open", []string{url}
	default:
		return "xdg-open", []string{url}
	}
}

// openBrowser best-effort opens url in the default browser. It never blocks on
// the browser process (Start, not Run) and never fails the caller: an error is
// reported to stderr so the server keeps running with the printed URL as the
// fallback (headless/CI/SSH).
func openBrowser(url string) {
	name, args := browserCommand(runtime.GOOS, url)
	if err := exec.Command(name, args...).Start(); err != nil {
		fmt.Fprintf(os.Stderr, "cerne: não consegui abrir o browser (%v) — acesse %s manualmente\n", err, url)
	}
}
