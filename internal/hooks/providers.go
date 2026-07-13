package hooks

// Provider adapters (F-0174): each install function writes the assets and
// wiring the provider's extension surface supports, per the researched
// capability matrix (R-0035 Codex, R-0036 Gemini, R-0037 Cursor). Every
// capability the surface cannot host becomes an explicit StatusSkipped result
// with the manual step (or the limitation) as the note — degradation is
// reported, never silent. Implemented by the tasks that follow the
// providerAssets refactor.

func installCursor(projDir string) []Result { return nil }

func installCodex(projDir string) []Result { return nil }

func installGemini(projDir string) []Result { return nil }
