// Command forecaddie-reconciler runs beside Caddy on each node. It pulls
// desired bindings from the coordinator, renders the Caddy fragment,
// validates, reloads, and reports health. See docs/design.md.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "forecaddie-reconciler: design phase, nothing to run yet — see docs/design.md")
	os.Exit(2)
}
