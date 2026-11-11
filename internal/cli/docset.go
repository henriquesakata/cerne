package cli

import "github.com/henriquesakata/cerne/internal/doccheck"

// hasRealContent aliases doccheck.HasRealContent; the implementation lives
// in internal/doccheck so internal/lint and internal/app can share the stub
// detection. Feature doc-set resolution itself moved to internal/app
// (app.FeatureDocSet), shared by the CLI transitions and the UI server.
var hasRealContent = doccheck.HasRealContent
