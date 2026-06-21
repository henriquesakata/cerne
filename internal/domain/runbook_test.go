package domain_test

import (
	"errors"
	"testing"

	"gitlab.com/rockt/cerne/internal/domain"
)

func TestTransitionRunbook(t *testing.T) {
	to, err := domain.TransitionRunbook(domain.RunbookActive, domain.ActionSupersede)
	if err != nil {
		t.Fatalf("active->supersede: %v", err)
	}
	if to != domain.RunbookSuperseded {
		t.Fatalf("active->supersede = %s, want superseded", to)
	}
}

func TestTransitionRunbookTerminal(t *testing.T) {
	if _, err := domain.TransitionRunbook(domain.RunbookSuperseded, domain.ActionSupersede); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("supersede from terminal err = %v, want ErrInvalidTransition", err)
	}
}

func TestTransitionRunbookUnknownState(t *testing.T) {
	if _, err := domain.TransitionRunbook(domain.RunbookState("bogus"), domain.ActionSupersede); !errors.Is(err, domain.ErrUnknownState) {
		t.Fatalf("unknown state err = %v, want ErrUnknownState", err)
	}
}

func TestTransitionRunbookInvalidAction(t *testing.T) {
	if _, err := domain.TransitionRunbook(domain.RunbookActive, domain.ActionApprove); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("invalid action err = %v, want ErrInvalidTransition", err)
	}
}
