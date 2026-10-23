// Package render turns registry state into the two Caddyfile fragments a
// reconciler owns (http.caddy and l4.caddy). Output is deterministic: the
// same state yields byte-identical files. Not yet implemented.
package render
