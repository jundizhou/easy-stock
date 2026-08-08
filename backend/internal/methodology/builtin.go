package methodology

import "embed"

// builtinMasteryFS is the offline baseline shipped with every backend binary.
// GitHub remains the update source, but first launch never depends on network access.
//
//go:embed builtin/manifest.json builtin/documents/*.md
var builtinMasteryFS embed.FS
