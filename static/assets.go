// Package static embeds the production frontend assets in the Go binary.
package static

import "embed"

// Files contains the compiled CSS, JavaScript and Datastar client.
//
//go:embed *.css *.js
var Files embed.FS
