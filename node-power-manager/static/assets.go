package static

import "embed"

//go:embed *.html *.js
var FS embed.FS
