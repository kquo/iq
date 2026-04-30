package main

import (
	"fmt"
	"os"

	"iq/internal/preptool"
)

func main() {
	cfg, help, err := preptool.ParseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if help {
		fmt.Print(preptool.Usage())
		return
	}
	cfg.Out = os.Stdout
	if err := preptool.Run(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
