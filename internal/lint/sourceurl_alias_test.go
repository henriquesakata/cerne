package lint

import "github.com/henriquesakata/cerne/internal/sourceurl"

// ignoredSourceURL keeps TestIgnoredSourceURL (F-0107) pinning the filter that
// moved to sourceurl (F-0503) without editing it.
var ignoredSourceURL = sourceurl.Internal
