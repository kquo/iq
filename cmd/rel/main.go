// CODE-flavor library wrapper. CODE consumers are Go projects with `go.mod`;
// the library import (`github.com/queone/governa-reltool`) has zero friction.
// DOC overlay uses an inline stdlib-only form because content repos shouldn't
// be required to be Go modules. See `docs/build-release.md` for the divergence
// rationale.
package main

import (
	"fmt"
	"os"

	"github.com/queone/governa-reltool"
)

func main() {
	cfg, help, err := reltool.ParseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if help {
		fmt.Print(reltool.Usage())
		return
	}
	if err := reltool.Run(cfg, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
