package cli

import "testing"

func TestUICmdPullIntervalFlag(t *testing.T) {
	cmd := newUICmd()
	f := cmd.Flags().Lookup("pull-interval")
	if f == nil {
		t.Fatal("ui command is missing the --pull-interval flag")
	}
	if f.DefValue != "15s" {
		t.Fatalf("default pull-interval = %q, want 15s", f.DefValue)
	}
	// 0 disables the periodic pull (offline-by-default).
	if err := cmd.Flags().Set("pull-interval", "0"); err != nil {
		t.Fatalf("Set pull-interval=0: %v", err)
	}
	d, err := cmd.Flags().GetDuration("pull-interval")
	if err != nil {
		t.Fatalf("GetDuration: %v", err)
	}
	if d != 0 {
		t.Fatalf("pull-interval = %v, want 0", d)
	}
}
