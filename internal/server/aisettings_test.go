package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/henriquesakata/cerne/internal/llm"
	"github.com/henriquesakata/cerne/internal/project"
)

const aiSecret = "sk-live-NEVER-SHOW-THIS-1234"

type fakeProv struct {
	reply string
	err   error
}

func (f fakeProv) Complete(context.Context, string) (string, error) { return f.reply, f.err }

// aiServer is a local ui server over a project with no [llm], an isolated
// config dir and a stubbed PATH probe.
func aiServer(t *testing.T, found ...string) (http.Handler, string) {
	t.Helper()
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	for _, v := range []string{"CERNE_LLM_API_KEY", "ANTHROPIC_API_KEY", "OPENAI_API_KEY", "GEMINI_API_KEY", "OPENROUTER_API_KEY", "CERNE_COMPATIBLE_API_KEY"} {
		t.Setenv(v, "")
	}
	repoDir, projDir := newBackend(t)
	if err := project.SaveConfig(projDir, project.Config{ProjectID: "p", Backend: "b"}); err != nil {
		t.Fatal(err)
	}
	origF, origD := project.FoundSubscriptionCLIs, project.DetectSubscriptionCLI
	t.Cleanup(func() { project.FoundSubscriptionCLIs, project.DetectSubscriptionCLI = origF, origD })
	project.FoundSubscriptionCLIs = func() []string { return found }
	project.DetectSubscriptionCLI = func() string {
		if len(found) > 0 {
			return found[0]
		}
		return ""
	}
	return New(repoDir, projDir, ""), projDir
}

// aiResult decodes the {ok,error} answer of the connection test.
func aiResult(t *testing.T, body string) (bool, string) {
	t.Helper()
	var r struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("%s: %v", body, err)
	}
	return r.OK, r.Error
}

func aiDo(t *testing.T, h http.Handler, method, path, body string) (int, string) {
	t.Helper()
	req := localRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

type aiStatus struct {
	Mode     string `json:"mode"`
	Source   string `json:"source"`
	Provider string `json:"provider"`
	Model    string `json:"model"`
	BaseURL  string `json:"base_url"`
	CLIs     []struct {
		Name  string `json:"name"`
		Mode  string `json:"mode"`
		Found bool   `json:"found"`
	} `json:"clis"`
	Key struct {
		Env    string `json:"env"`
		Set    bool   `json:"set"`
		Source string `json:"source"`
		Last4  string `json:"last4"`
	} `json:"key"`
}

func TestAIStatusReportsDetectionAndKeyWithoutTheValue(t *testing.T) {
	h, _ := aiServer(t, "codex")
	code, body := aiDo(t, h, http.MethodGet, "/api/config/llm", "")
	var st aiStatus
	if code != 200 || json.Unmarshal([]byte(body), &st) != nil {
		t.Fatalf("%d %s", code, body)
	}
	if st.Mode != "codex-cli" || st.Source != "detected" || st.Provider != "anthropic" || st.Key.Env != "ANTHROPIC_API_KEY" || st.Key.Set {
		t.Fatalf("status = %+v", st)
	}
	found := map[string]bool{}
	for _, c := range st.CLIs {
		found[c.Mode] = c.Found
	}
	if len(st.CLIs) != 5 || !found["codex-cli"] || found["claude-cli"] || found["gemini-cli"] || found["cursor-cli"] || found["copilot-cli"] {
		t.Fatalf("clis = %+v", st.CLIs)
	}

	t.Setenv("CERNE_LLM_API_KEY", aiSecret)
	_, body = aiDo(t, h, http.MethodGet, "/api/config/llm", "")
	json.Unmarshal([]byte(body), &st)
	if !st.Key.Set || st.Key.Source != "env" || st.Key.Last4 != "1234" || strings.Contains(body, aiSecret) {
		t.Fatalf("env key: %+v in %s", st.Key, body)
	}

	t.Setenv("CERNE_LLM_API_KEY", "")
	if err := project.WriteCredential("CERNE_LLM_API_KEY", aiSecret); err != nil {
		t.Fatal(err)
	}
	_, body = aiDo(t, h, http.MethodGet, "/api/config/llm", "")
	json.Unmarshal([]byte(body), &st)
	if !st.Key.Set || st.Key.Source != "file" || strings.Contains(body, aiSecret) {
		t.Fatalf("file key: %+v in %s", st.Key, body)
	}
}

func TestAIConnectionTest(t *testing.T) {
	h, _ := aiServer(t, "claude")
	var gotMode string
	orig := newLLMProvider
	t.Cleanup(func() { newLLMProvider = orig })
	reply, perr := "ok", error(nil)
	newLLMProvider = func(cfg project.LLMConfig) (llm.Provider, error) {
		gotMode = cfg.Mode
		return fakeProv{reply, perr}, nil
	}

	_, body := aiDo(t, h, http.MethodPost, "/api/config/llm/test", `{"mode":"codex-cli"}`)
	if ok, _ := aiResult(t, body); gotMode != "codex-cli" || !ok {
		t.Fatalf("requested mode: got %q, body %s", gotMode, body)
	}
	aiDo(t, h, http.MethodPost, "/api/config/llm/test", `{}`)
	if gotMode != "" {
		t.Fatalf("no mode requested: provider got %q, want the config's (empty)", gotMode)
	}

	perr = errors.New("codex-cli invocation failed: run 'codex login'")
	_, body = aiDo(t, h, http.MethodPost, "/api/config/llm/test", `{}`)
	if ok, msg := aiResult(t, body); ok || !strings.Contains(msg, "codex login") {
		t.Fatalf("failure must carry the fix, got %s", body)
	}

	newLLMProvider = func(project.LLMConfig) (llm.Provider, error) { return nil, llm.ErrNoProviderForDelegation }
	_, body = aiDo(t, h, http.MethodPost, "/api/config/llm/test", `{"mode":"delegation"}`)
	if ok, msg := aiResult(t, body); ok || !strings.Contains(msg, "delegation") {
		t.Fatalf("delegation: %s", body)
	}
	if code, _ := aiDo(t, h, http.MethodPost, "/api/config/llm/test", `{"mode":"nope"}`); code != 400 {
		t.Fatalf("invalid mode: %d", code)
	}
}

func TestAIKeyPutDelete(t *testing.T) {
	h, _ := aiServer(t)
	code, body := aiDo(t, h, http.MethodPut, "/api/config/llm/key", `{"provider":"openrouter","value":"`+aiSecret+`"}`)
	if code != 200 || strings.Contains(body, aiSecret) {
		t.Fatalf("PUT: %d %s", code, body)
	}
	var k struct {
		Env, Source, Last4 string
		Set                bool
	}
	json.Unmarshal([]byte(body), &k)
	if k.Env != "OPENROUTER_API_KEY" || !k.Set || k.Source != "file" || k.Last4 != "1234" {
		t.Fatalf("PUT body = %s", body)
	}
	dir := os.Getenv("CERNE_CONFIG_DIR")
	raw, err := os.ReadFile(filepath.Join(dir, "credentials.env"))
	if err != nil || string(raw) != "OPENROUTER_API_KEY="+aiSecret+"\n" {
		t.Fatalf("file = %q (%v)", raw, err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(filepath.Join(dir, "credentials.env")); fi.Mode().Perm() != 0o600 {
			t.Fatalf("mode %v", fi.Mode().Perm())
		}
	}
	// Never in the project or the settings files.
	if b, _ := os.ReadFile(filepath.Join(dir, "settings.toml")); strings.Contains(string(b), aiSecret) {
		t.Fatal("key leaked into settings.toml")
	}

	code, body = aiDo(t, h, http.MethodDelete, "/api/config/llm/key", `{"provider":"openrouter"}`)
	json.Unmarshal([]byte(body), &k)
	if code != 200 || k.Set || strings.Contains(body, aiSecret) {
		t.Fatalf("DELETE: %d %s", code, body)
	}
	if v, _, _ := project.ResolveAPIKey("OPENROUTER_API_KEY"); v != "" {
		t.Fatal("key still stored")
	}

	for _, bad := range []string{`{"value":""}`, `{"value":"a\nb"}`, `{"provider":"nope","value":"x"}`, `not json`} {
		if code, body := aiDo(t, h, http.MethodPut, "/api/config/llm/key", bad); code != 400 || strings.Contains(body, "a\nb") {
			t.Errorf("PUT %s: %d %s", bad, code, body)
		}
	}
}

func TestAIRoutesAreLocalOnly(t *testing.T) {
	want := map[string]bool{"POST /config/llm/test": false, "PUT /config/llm/key": false, "DELETE /config/llm/key": false}
	for _, r := range dataRoutes() {
		k := r.method + " " + r.pattern
		if _, ok := want[k]; ok {
			if r.remote {
				t.Errorf("%s is remote, want local-only", k)
			}
			want[k] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("%s not declared in dataRoutes()", k)
		}
	}
}

// F-0414: the test runs the unsaved form. The typed key is used for that one
// call, in memory: not stored, not logged, not echoed.
func TestAIConnectionTestUsesTheUnsavedForm(t *testing.T) {
	h, _ := aiServer(t)
	var gotAuth, gotModel string
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		var body struct {
			Model string `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		gotModel = body.Model
		w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer api.Close()

	form := `{"provider":"openrouter","model":"m-1","base_url":"` + api.URL + `","key":"` + aiSecret + `"}`
	_, body := aiDo(t, h, http.MethodPost, "/api/config/llm/test", form)
	if ok, msg := aiResult(t, body); !ok {
		t.Fatalf("unsaved byo-key test failed: %s", msg)
	}
	if gotAuth != "Bearer "+aiSecret || gotModel != "m-1" {
		t.Fatalf("provider saw auth=%q model=%q", gotAuth, gotModel)
	}
	if strings.Contains(body, aiSecret) {
		t.Fatalf("response echoes the key: %s", body)
	}
	// Nothing was stored: no credentials file, and a status read has no key.
	_, status := aiDo(t, h, http.MethodGet, "/api/config/llm", "")
	if strings.Contains(status, aiSecret) || strings.Contains(status, `"set":true`) {
		t.Fatalf("the typed key was kept: %s", status)
	}
	if v, _, _ := project.ResolveAPIKey("OPENROUTER_API_KEY"); v != "" {
		t.Fatal("the typed key reached the credentials file")
	}

	// A failing call does not echo the key either.
	_, body = aiDo(t, h, http.MethodPost, "/api/config/llm/test", `{"provider":"openrouter","model":"m","base_url":"http://127.0.0.1:1","key":"`+aiSecret+`"}`)
	if ok, _ := aiResult(t, body); ok || strings.Contains(body, aiSecret) {
		t.Fatalf("unreachable endpoint: %s", body)
	}
}


// saveLLM writes a project [llm] the way a team's .cernerc would carry it.
func saveLLM(t *testing.T, projDir string, l project.LLMConfig) {
	t.Helper()
	cfg, _, err := project.LoadConfig(projDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.LLM = l
	if err := project.SaveConfig(projDir, cfg); err != nil {
		t.Fatal(err)
	}
}

func chatServer(t *testing.T, hits *int, auth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		*auth = r.Header.Get("Authorization")
		w.Write([]byte(`{"choices":[{"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// F-0414 (D4): the status carries every provider's key state, never a value.
func TestAIStatusCarriesEveryProvidersKey(t *testing.T) {
	h, _ := aiServer(t)
	t.Setenv("OPENAI_API_KEY", aiSecret)
	_, body := aiDo(t, h, http.MethodGet, "/api/config/llm", "")
	var st struct {
		Keys map[string]struct {
			Env, Source, Last4 string
			Set                bool
		} `json:"keys"`
	}
	if err := json.Unmarshal([]byte(body), &st); err != nil || strings.Contains(body, aiSecret) {
		t.Fatalf("%v %s", err, body)
	}
	if k := st.Keys["openai"]; !k.Set || k.Env != "OPENAI_API_KEY" || k.Source != "env" || k.Last4 != "1234" {
		t.Fatalf("openai = %+v", k)
	}
	if st.Keys["anthropic"].Set || st.Keys["compatible"].Set || len(st.Keys) != 5 {
		t.Fatalf("keys = %+v", st.Keys)
	}
}

// F-0414 (B2): saving one provider's key never overwrites another's.
func TestAIKeysAreSavedUnderEachProvidersOwnName(t *testing.T) {
	h, _ := aiServer(t)
	aiDo(t, h, http.MethodPut, "/api/config/llm/key", `{"provider":"anthropic","value":"sk-ant-0001"}`)
	aiDo(t, h, http.MethodPut, "/api/config/llm/key", `{"provider":"openai","value":"sk-oai-0002"}`)
	raw, _ := os.ReadFile(filepath.Join(os.Getenv("CERNE_CONFIG_DIR"), "credentials.env"))
	for _, want := range []string{"ANTHROPIC_API_KEY=sk-ant-0001", "OPENAI_API_KEY=sk-oai-0002"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("credentials = %q, missing %q", raw, want)
		}
	}
}

// F-0414 (B3): the model shown is the one the mode in use receives.
func TestAIStatusModelIsTheOneTheModeUses(t *testing.T) {
	cases := []struct {
		l    project.LLMConfig
		want string
	}{
		{project.LLMConfig{Mode: "claude-cli", Model: "gpt-5"}, ""},
		{project.LLMConfig{Mode: "codex-cli", Model: "gpt-5"}, "gpt-5"},
		{project.LLMConfig{Mode: "codex-cli", Provider: "openai", Model: "gpt-5"}, ""},
		{project.LLMConfig{Mode: "byo-key", Provider: "openai", Model: "gpt-5"}, "gpt-5"},
	}
	for _, c := range cases {
		h, dir := aiServer(t)
		saveLLM(t, dir, c.l)
		_, body := aiDo(t, h, http.MethodGet, "/api/config/llm", "")
		var st aiStatus
		json.Unmarshal([]byte(body), &st)
		if st.Model != c.want {
			t.Errorf("%+v: model = %q, want %q", c.l, st.Model, c.want)
		}
	}
}

// F-0414 (M8): the remote `cerne serve` never learns the server's key state or CLIs.
func TestAIStatusOmitsKeysAndCLIsWhenServedRemotely(t *testing.T) {
	_, dir := aiServer(t, "claude")
	t.Setenv("OPENAI_API_KEY", aiSecret)
	req := localRequest(http.MethodGet, "/api/config/llm", nil)
	req = req.WithContext(context.WithValue(req.Context(), remoteKey{}, true))
	rec := httptest.NewRecorder()
	handleLLMConfigGet(rec, req, dir)
	body := rec.Body.String()
	for _, banned := range []string{`"key"`, `"keys"`, `"clis"`, "last4", aiSecret} {
		if strings.Contains(body, banned) {
			t.Fatalf("remote status leaks %s: %s", banned, body)
		}
	}
	if !strings.Contains(body, `"mode"`) {
		t.Fatalf("remote status lost the mode: %s", body)
	}
}

// F-0414 (D3): without a typed key the test uses the key the saved config
// would, and never hands a stored key to an endpoint other than the saved one.
func TestAITestUsesTheSavedKeyEnvAndGuardsTheEndpoint(t *testing.T) {
	h, dir := aiServer(t)
	var hits int
	var auth string
	api := chatServer(t, &hits, &auth)
	t.Setenv("MY_OPENAI_KEY", "sk-mine-0003")
	saveLLM(t, dir, project.LLMConfig{Mode: "byo-key", Provider: "openai", Model: "m", KeyEnv: "MY_OPENAI_KEY", BaseURL: api.URL})

	// Same provider, same URL: the saved key_env is read.
	_, body := aiDo(t, h, http.MethodPost, "/api/config/llm/test", `{"provider":"openai","model":"m","base_url":"`+api.URL+`"}`)
	if ok, msg := aiResult(t, body); !ok || auth != "Bearer sk-mine-0003" {
		t.Fatalf("saved key_env: ok=%v %q auth=%q", ok, msg, auth)
	}

	// Another URL: the stored key is not sent there.
	hits = 0
	other := chatServer(t, &hits, &auth)
	_, body = aiDo(t, h, http.MethodPost, "/api/config/llm/test", `{"provider":"openai","model":"m","base_url":"`+other.URL+`"}`)
	if ok, msg := aiResult(t, body); ok || !strings.Contains(msg, "base URL") || hits != 0 {
		t.Fatalf("different base URL must refuse: ok=%v %q hits=%d", ok, msg, hits)
	}
	// compatible never gets another provider's key, saved or not.
	t.Setenv("CERNE_LLM_API_KEY", "sk-anthropic-legacy")
	_, body = aiDo(t, h, http.MethodPost, "/api/config/llm/test", `{"provider":"compatible","model":"m","base_url":"`+other.URL+`"}`)
	if ok, _ := aiResult(t, body); !ok || auth != "" {
		t.Fatalf("compatible got a foreign key: ok=%v auth=%q", ok, auth)
	}
}

func TestAITestWithATypedKeyNamesAnEnvVarThatWins(t *testing.T) {
	h, _ := aiServer(t)
	var hits int
	var auth string
	api := chatServer(t, &hits, &auth)
	t.Setenv("OPENROUTER_API_KEY", "sk-env-0004")
	_, body := aiDo(t, h, http.MethodPost, "/api/config/llm/test", `{"provider":"openrouter","model":"m","base_url":"`+api.URL+`","key":"`+aiSecret+`"}`)
	var r struct {
		OK   bool   `json:"ok"`
		Note string `json:"note"`
	}
	json.Unmarshal([]byte(body), &r)
	if !r.OK || auth != "Bearer "+aiSecret || !strings.Contains(r.Note, "OPENROUTER_API_KEY") || strings.Contains(body, aiSecret) {
		t.Fatalf("ok=%v auth=%q note=%q body=%s", r.OK, auth, r.Note, body)
	}
}

// B-0092: an Anthropic key in the shared legacy variable is not reported as
// the openai provider's key.
func TestKeyStatusDoesNotLendALegacyKeyToAnotherVendor(t *testing.T) {
	t.Setenv("CERNE_CONFIG_DIR", t.TempDir())
	for _, n := range []string{"ANTHROPIC_API_KEY", "OPENAI_API_KEY"} {
		t.Setenv(n, "")
	}
	t.Setenv("CERNE_LLM_API_KEY", "sk-ant-api03-SECRETSECRET")
	st := keyStatuses(project.LLMConfig{Mode: "byo-key"})
	if !st["anthropic"].Set {
		t.Fatalf("anthropic must still see its key in the legacy variable: %+v", st["anthropic"])
	}
	if o := st["openai"]; o.Set || o.Problem != "" || o.Last4 != "" {
		t.Fatalf("openai reports another vendor's key: %+v", o)
	}
}
