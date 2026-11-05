// Package static embeds the production frontend assets in the Go binary.
package static

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"sync"
)

// Files contains the compiled CSS, JavaScript and Datastar client.
//
//go:embed *.css *.js
var Files embed.FS

// versions is each file's version: the start of its content's SHA-256.
var versions = sync.OnceValue(func() map[string]string {
	v := map[string]string{}
	entries, _ := fs.ReadDir(Files, ".")
	for _, e := range entries {
		content, err := Files.ReadFile(e.Name())
		if err != nil {
			continue
		}
		sum := sha256.Sum256(content)
		v[e.Name()] = hex.EncodeToString(sum[:])[:16]
	}
	return v
})

// URL is the address of the file called name, with its version: a browser
// that kept an older build's file fetches this one instead.
func URL(name string) string {
	if v, ok := versions()[name]; ok {
		return "/static/" + name + "?v=" + v
	}
	return "/static/" + name
}
