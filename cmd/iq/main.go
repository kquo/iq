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
	programName    = "iq"
	programVersion = "0.9.3"
)

// errSilent is returned when the error has already been printed.
// Using a named type ensures errors.Is comparisons work correctly.
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
	fmt.Printf("Work with IQ from the command line.\n\n")
	fmt.Printf("%s\n", color.Whi2("USAGE"))
	fmt.Printf("  %s <command> [subcommand] [flags]\n", n)
	fmt.Printf("  %s [flags] <message>\n\n", n)
	fmt.Printf("%s\n", color.Whi2("SERVICE"))
	fmt.Printf("  %-24s %s\n", "start [tier|model]", "Start sidecars")
	fmt.Printf("  %-24s %s\n", "stop [tier|model]", "Stop sidecars")
	fmt.Printf("  %-24s %s\n", "st|status", "Show running sidecar status")
	fmt.Printf("  %-24s %s\n", "doc", "Check runtime dependencies and model readiness")
	fmt.Printf("  %-24s %s\n", "tier", "Manage model tier pool assignments")
	fmt.Printf("  %-24s %s\n", "embed", "Manage embed sidecar model")
	fmt.Printf("  %-24s %s\n\n", "cfg|config", "Inspect and validate IQ configuration")
	fmt.Printf("%s\n", color.Whi2("COMMANDS"))
	fmt.Printf("  %-24s %s\n", "lm", "Work with IQ language models")
	fmt.Printf("  %-24s %s\n", "ask", "Interactive REPL and prompt aliases")
	fmt.Printf("  %-24s %s\n", "cue", "Work with IQ cues")
	fmt.Printf("  %-24s %s\n", "kb", "Work with IQ knowledge base")
	fmt.Printf("  %-24s %s\n", "perf", "Benchmark IQ model performance")
	fmt.Printf("  %-24s %s\n", "pry", "Send a raw message directly to a model sidecar")
	fmt.Printf("  %-24s %s\n\n", "version", "Show the current IQ version")
	fmt.Printf("%s\n", color.Whi2("FLAGS"))
	fmt.Printf("  %-24s %s\n", "-r, --cue <n>", "Skip classification, use this cue")
	fmt.Printf("  %-24s %s\n", "-c, --category <n>", "Classify within a category only")
	fmt.Printf("  %-24s %s\n", "    --tier <n>", "Override tier directly, bypass cue system")
	fmt.Printf("  %-24s %s\n", "-s, --session <id>", "Load/continue a session by ID")
	fmt.Printf("  %-24s %s\n", "-n, --dry-run", "Trace steps 1–4, skip inference")
	fmt.Printf("  %-24s %s\n", "    --dump-prompt <f>", "Write assembled messages as JSON (- for stdout)")
	fmt.Printf("  %-24s %s\n", "-d, --debug", "Trace all steps including inference")
	fmt.Printf("  %-24s %s\n", "-K, --no-kb", "Disable knowledge base retrieval for this prompt")
	fmt.Printf("  %-24s %s\n", "    --no-cache", "Disable response cache")
	fmt.Printf("  %-24s %s\n", "-T, --tools", "Force enable read-only tool use")
	fmt.Printf("  %-24s %s\n", "    --no-tools", "Disable tool use")
	fmt.Printf("  %-24s %s\n", "    --no-stream", "Collect full response before printing")
	fmt.Printf("  %-24s %s\n", "-h, -?, --help", "Show this help output or the help for a specified subcommand.")
	fmt.Printf("  %-24s %s\n\n", "-v, --version", "An alias for the \"version\" subcommand.")
	fmt.Printf("%s\n", color.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s \"explain transformers\"\n", n)
	fmt.Printf("  $ %s -d \"explain transformers\"\n", n)
	fmt.Printf("  $ %s ask\n", n)
	fmt.Printf("  $ %s start\n", n)
	fmt.Printf("  $ %s stop\n", n)
	fmt.Printf("  $ %s st\n", n)
	fmt.Printf("  $ %s doc\n", n)
}

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "version",
		Aliases: []string{"ver"},
		Short:   "Show the current IQ version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("%s v%s\n", programName, programVersion)
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s v%s\n", programName, programVersion)
	})
	return cmd
}

// newStatusCmd returns a top-level `iq status` / `iq st` command.
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

	// Rewrite "iq -h <cmd>" → "iq <cmd> -h" so cobra routes correctly.
	if len(os.Args) == 3 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		os.Args = []string{os.Args[0], os.Args[2], "-h"}
	}

	var rootOpts promptOpts

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
			input := strings.Join(args, " ")
			var sess *session
			if rootOpts.sessionID != "" {
				var err error
				sess, err = loadSession(rootOpts.sessionID)
				if err != nil {
					return err
				}
			}
			_, err := executePrompt(ctx, input, rootOpts, sess)
			return err
		},
	}

	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printRootHelp()
	})
	root.CompletionOptions.DisableDefaultCmd = true
	root.Flags().BoolP("version", "v", false, "An alias for the \"version\" subcommand.")
	addPromptFlags(root, &rootOpts)

	root.AddCommand(
		newVersionCmd(),
		newStartCmd(),
		newStopCmd(),
		newStatusCmd(),
		newDocCmd(),
		newTierCmd(),
		newEmbedCmd(),
		newLmCmd(),
		newPromptCmd(),
		newCueCmd(),
		newKbCmd(),
		newPerfCmd(),
		newConfigCmd(),
		newProbeCmd(),
		newSvcCmd(), // hidden backward-compat alias
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
