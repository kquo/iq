// Thin wrapper. Logic lives in github.com/queone/governa-reltool.
// Kept in-tree (not extracted to the library's cmd/) because build.sh invokes
// via `go run ./cmd/rel` — extraction would move version pinning into build.sh
// + build.sh.tmpl, a worse propagation surface than this ~20 lines of inert
// boilerplate. See AC102 reasoning.
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
