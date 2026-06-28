package domain

import (
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeFacets(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr error
	}{
		{"lowercases", []string{"Infra"}, []string{"infra"}, nil},
		{"trims and lowercases", []string{"  Code "}, []string{"code"}, nil},
		{"dedup case-insensitive", []string{"infra", "INFRA"}, []string{"infra"}, nil},
		{"keeps order, multiple", []string{"infra", "code", "data"}, []string{"infra", "code", "data"}, nil},
		{"skips empty entries", []string{"infra", "  ", ""}, []string{"infra"}, nil},
		{"nil in, empty out", nil, nil, nil},
		{"unknown rejected", []string{"prod"}, nil, ErrUnknownFacet},
		{"unknown among valid rejected", []string{"infra", "homolog"}, nil, ErrUnknownFacet},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeFacets(tt.in)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("err = %v, want errors.Is %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if len(got) != 0 || len(tt.want) != 0 {
				if !reflect.DeepEqual(got, tt.want) {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestNormalizeComponents(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{"slug lowercases", []string{"Gitea", "KPack"}, []string{"gitea", "kpack"}},
		{"dedup after slug", []string{"gitea", "Gitea"}, []string{"gitea"}},
		{"space to dash", []string{"K Pack"}, []string{"k-pack"}},
		{"strips junk runes", []string{"k8s!"}, []string{"k8s"}},
		{"skips entries that slug to empty", []string{"gitea", "!!!", ""}, []string{"gitea"}},
		{"nil in, empty out", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeComponents(tt.in)
			if len(got) != 0 || len(tt.want) != 0 {
				if !reflect.DeepEqual(got, tt.want) {
					t.Fatalf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}
