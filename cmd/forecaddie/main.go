// Command forecaddie is the coordinator: registry, approval state machine,
// policy writer, API and UI. See docs/design.md.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "forecaddie: design phase, nothing to run yet — see docs/design.md")
	os.Exit(2)
}
