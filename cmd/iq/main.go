package main

import (
	"fmt"
	"os"

	"github.com/queone/utl"
	"github.com/spf13/cobra"
)

const (
	program_name    = "iq"
	program_version = "0.4.6"
)

func printRootHelp() {
	n := program_name
	fmt.Printf("%s v%s\n", n, program_version)
	fmt.Printf("Work with IQ from the command line.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s <command> <subcommand> [flags]\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-15s %s\n", "svc", "Work with IQ service daemon")
	fmt.Printf("  %-15s %s\n", "lm", "Work with IQ language models")
	fmt.Printf("  %-15s %s\n", "ask", "Ask a question or send a prompt to a local IQ model")
	fmt.Printf("  %-15s %s\n", "cue", "Work with IQ cues")
	fmt.Printf("  %-15s %s\n", "kb", "Work with IQ knowledge base")
	fmt.Printf("  %-15s %s\n", "perf", "Benchmark IQ model performance")
	fmt.Printf("  %-15s %s\n", "pry", "Send a raw message directly to a model sidecar")
	fmt.Printf("  %-15s %s\n", "status", "Show running sidecar status (alias: st)")
	fmt.Printf("  %-15s %s\n\n", "version", "Show the current IQ version")
	fmt.Printf("%s\n", utl.Whi2("FLAGS"))
	fmt.Printf("  %-30s %s\n", "-h, --help", "Show this help output or the help for a specified subcommand.")
	fmt.Printf("  %-30s %s\n\n", "-v, --version", "An alias for the \"version\" subcommand.")
}

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "version",
		Aliases: []string{"ver"},
		Short:   "Show the current IQ version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("%s v%s\n", program_name, program_version)
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s v%s\n", program_name, program_version)
	})
	return cmd
}

// newStatusCmd returns a top-level `iq status` / `iq st` command that delegates
// to the same printStatus() logic as `iq svc status`.
func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "status",
		Aliases:      []string{"st"},
		Short:        "Show running sidecar status (shortcut for 'iq svc status')",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return printStatus()
		},
	}
}

func runCLI() {
	// Rewrite "iq -h <cmd>" → "iq <cmd> -h" so cobra routes correctly.
	if len(os.Args) == 3 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		os.Args = []string{os.Args[0], os.Args[2], "-h"}
	}

	root := &cobra.Command{
		Use:          program_name,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if v, _ := cmd.Flags().GetBool("version"); v {
				fmt.Printf("%s v%s\n", program_name, program_version)
				return nil
			}
			printRootHelp()
			return nil
		},
	}

	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printRootHelp()
	})
	root.CompletionOptions.DisableDefaultCmd = true
	root.Flags().BoolP("version", "v", false, "An alias for the \"version\" subcommand.")

	root.AddCommand(
		newVersionCmd(),
		newSvcCmd(),
		newLmCmd(),
		newPromptCmd(),
		newCueCmd(),
		newKbCmd(),
		newPerfCmd(),
		newProbeCmd(),
		newStatusCmd(),
	)

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func main() {
	runCLI()
}
