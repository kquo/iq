package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/spf13/cobra"
	"iq/internal/color"
)

const (
	programName    = "kb"
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
	fmt.Printf("Private knowledge base — ingest, search, ask.\n\n")
	fmt.Printf("%s\n", color.Whi2("USAGE"))
	fmt.Printf("  %s <command> [flags]\n", n)
	fmt.Printf("  %s [flags] <query>\n\n", n)
	fmt.Printf("%s\n", color.Whi2("SERVICE"))
	fmt.Printf("  %-24s %s\n", "start [model]", "Start sidecars")
	fmt.Printf("  %-24s %s\n", "stop [model]", "Stop sidecars")
	fmt.Printf("  %-24s %s\n", "restart [model]", "Restart sidecars (stop + start)")
	fmt.Printf("  %-24s %s\n\n", "st|status", "Show running sidecar status")
	fmt.Printf("%s\n", color.Whi2("KNOWLEDGE BASE"))
	fmt.Printf("  %-24s %s\n", "ingest, in <path>", "Ingest a file or directory tree")
	fmt.Printf("  %-24s %s\n", "list", "Show indexed sources")
	fmt.Printf("  %-24s %s\n", "search <query>", "Raw similarity search (no inference)")
	fmt.Printf("  %-24s %s\n", "rm <path>", "Remove a source from the index")
	fmt.Printf("  %-24s %s\n\n", "clear", "Wipe the knowledge base")
	fmt.Printf("%s\n", color.Whi2("COMMANDS"))
	fmt.Printf("  %-24s %s\n", "ask <query>", "Ask using KB-grounded inference")
	fmt.Printf("  %-24s %s\n", "cfg|config", "Inspect KB configuration")
	fmt.Printf("  %-24s %s\n\n", "version", "Show the current KB version")
	fmt.Printf("%s\n", color.Whi2("FLAGS"))
	fmt.Printf("  %-24s %s\n", "    --model <id>", "Override inference model (must be running)")
	fmt.Printf("  %-24s %s\n", "-K, --no-kb", "Skip KB retrieval, run pure inference")
	fmt.Printf("  %-24s %s\n", "-k, --top-k <n>", "Number of KB chunks to retrieve")
	fmt.Printf("  %-24s %s\n\n", "-h, -?, --help", "Show this help or help for a subcommand")
	fmt.Printf("%s\n", color.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s ingest ~/projects/notes\n", n)
	fmt.Printf("  $ %s list\n", n)
	fmt.Printf("  $ %s \"how does auth work\"\n", n)
	fmt.Printf("  $ %s ask \"explain the key concepts\"\n", n)
	fmt.Printf("  $ %s start && %s \"what is X?\"\n", n, n)
}

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "version",
		Aliases: []string{"ver"},
		Short:   "Show the current KB version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("%s v%s\n", programName, programVersion)
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s v%s\n", programName, programVersion)
	})
	return cmd
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Aliases:      []string{"st"},
		Short:        "Show running sidecar status",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return printStatus()
		},
	}
}

func runCLI() {
	// Rewrite "-?" → "-h" so cobra sees a standard help flag.
	for i := 1; i < len(os.Args); i++ {
		if os.Args[i] == "-?" {
			os.Args[i] = "-h"
		}
	}

	// Rewrite "kb -h <cmd>" → "kb <cmd> -h" so cobra routes correctly.
	if len(os.Args) == 3 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		os.Args = []string{os.Args[0], os.Args[2], "-h"}
	}

	var rootOpts askOpts

	root := &cobra.Command{
		Use:          programName,
		SilenceUsage: true,
		Args:         cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if v, _ := cmd.Flags().GetBool("version"); v {
				fmt.Printf("%s v%s\n", programName, programVersion)
				return nil
			}
			if len(args) == 0 {
				printRootHelp()
				return nil
			}
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()
			rootOpts.query = strings.Join(args, " ")
			return runAsk(ctx, rootOpts)
		},
	}

	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printRootHelp()
	})
	root.CompletionOptions.DisableDefaultCmd = true
	root.Flags().BoolP("version", "v", false, "An alias for the \"version\" subcommand.")
	addAskFlags(root, &rootOpts)

	root.AddCommand(
		newVersionCmd(),
		newStartCmd(),
		newStopCmd(),
		newRestartCmd(),
		newStatusCmd(),
		newKbIngestCmd(),
		newKbListCmd(),
		newKbSearchCmd(),
		newKbRmCmd(),
		newKbClearCmd(),
		newAskCmd(),
		newConfigCmd(),
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
