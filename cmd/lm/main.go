package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"iq/internal/color"
)

const (
	programName    = "lm"
	programVersion = "0.1.0"
)

// errSilent is returned when the error has already been printed.
type silentErr struct{}

func (silentErr) Error() string { return "" }

var errSilent error = silentErr{}

// argsUsage wraps a cobra arg validator to print yellow error + help on failure.
func argsUsage(v cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := v(cmd, args); err != nil {
			fmt.Fprintf(os.Stderr, "%s\n\n", color.Yel(err.Error()))
			cmd.Help()
			return errSilent
		}
		return nil
	}
}

func printRootHelp() {
	n := programName
	fmt.Printf("%s v%s\n", n, programVersion)
	fmt.Printf("Local model manager.\n\n")
	fmt.Printf("%s\n", color.Whi2("USAGE"))
	fmt.Printf("  %s <command> [flags]\n\n", n)
	fmt.Printf("%s\n", color.Whi2("MODELS"))
	fmt.Printf("  %-30s %s\n", "search [query|count]", "Search MLX model registry; numeric arg sets result count")
	fmt.Printf("  %-30s %s\n", "get <model>", "Download a model from the registry")
	fmt.Printf("  %-30s %s\n", "ls|list", "List locally available models")
	fmt.Printf("  %-30s %s\n", "show <model>", "Show details for a model")
	fmt.Printf("  %-30s %s\n\n", "rm <model>", "Remove a model")
	fmt.Printf("%s\n", color.Whi2("BENCHMARKING"))
	fmt.Printf("  %-30s %s\n\n", "perf [bench|sweep|show|clear]", "Benchmark model performance")
	fmt.Printf("%s\n", color.Whi2("FLAGS"))
	fmt.Printf("  %-30s %s\n\n", "-h, --help", "Show help for command")
	fmt.Printf("%s\n", color.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s search gemma\n", n)
	fmt.Printf("  $ %s get mlx-community/gemma-3-1b-it-4bit\n", n)
	fmt.Printf("  $ %s list\n", n)
	fmt.Printf("  $ %s show mlx-community/gemma-3-1b-it-4bit\n", n)
	fmt.Printf("  $ %s perf bench --type infer --model mlx-community/gemma-3-1b-it-4bit\n", n)
}

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "version",
		Aliases: []string{"ver"},
		Short:   "Show the current lm version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("%s v%s\n", programName, programVersion)
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s v%s\n", programName, programVersion)
	})
	return cmd
}

func runCLI() {
	root := &cobra.Command{
		Use:          programName,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			printRootHelp()
			return nil
		},
	}
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printRootHelp()
	})
	root.CompletionOptions.DisableDefaultCmd = true

	root.AddCommand(
		newVersionCmd(),
		newLmSearchCmd(),
		newLmGetCmd(),
		newLmListCmd(),
		newLmShowCmd(),
		newLmRmCmd(),
		newPerfCmd(),
	)

	root.SilenceErrors = true
	if err := root.Execute(); err != nil {
		if !errors.Is(err, errSilent) {
			fmt.Fprintf(os.Stderr, "Error: %s\n", err)
		}
		os.Exit(1)
	}
}

func main() {
	runCLI()
}
