package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/queone/utl"
	"github.com/spf13/cobra"
)

// ── Help ──────────────────────────────────────────────────────────────────────

func printProbeHelp() {
	n := program_name
	fmt.Printf("Send a raw message directly to a model sidecar, bypassing the IQ framework.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s probe <model|tier> [flags] <message>\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("FLAGS"))
	fmt.Printf("  %-30s %s\n", "-s, --system <text>", "Optional system prompt")
	fmt.Printf("  %-30s %s\n\n", "-S, --no-stream", "Collect full response before printing")
	fmt.Printf("%s\n", utl.Whi2("INHERITED FLAGS"))
	fmt.Printf("  %-30s %s\n\n", "-h, --help", "Show help for command")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s probe fast \"what is 2+2?\"\n", n)
	fmt.Printf("  $ %s probe slow \"explain gradient descent\"\n", n)
	fmt.Printf("  $ %s probe mlx-community/SmolLM2-135M-Instruct-8bit \"hello\"\n", n)
	fmt.Printf("  $ %s probe fast \"respond in pirate speak\" -s \"You are a pirate.\"\n\n", n)
}

// ── Command ───────────────────────────────────────────────────────────────────

func newProbeCmd() *cobra.Command {
	var system string
	var noStream bool

	cmd := &cobra.Command{
		Use:          "probe <model|tier> <message>",
		Short:        "Send a raw message directly to a model sidecar",
		SilenceUsage: true,
		Args:         cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := args[0]
			message := strings.Join(args[1:], " ")

			// Resolve sidecar — tier name or specific model ID.
			var sidecar *svcState
			var err error
			switch target {
			case "fast", "slow":
				sidecar, err = pickSidecar(target, false)
				if err != nil {
					return err
				}
			default:
				sidecar, err = readState(target)
				if err != nil {
					return err
				}
				if sidecar == nil || !pidAlive(sidecar.PID) {
					return fmt.Errorf("%s is not running — run 'iq svc start %s' first", target, target)
				}
			}

			// Print routing header in gray.
			fmt.Fprintf(os.Stderr, "%s\n",
				utl.Gra(fmt.Sprintf("[%s  %s  :%d]",
					sidecar.Tier, sidecar.Model, sidecar.Port)))

			// Build messages.
			var messages []chatMessage
			if system != "" {
				messages = append(messages, chatMessage{Role: "system", Content: system})
			}
			messages = append(messages, chatMessage{Role: "user", Content: message})

			// Infer and time it.
			t0 := time.Now()
			if noStream {
				response, err := callSidecar(sidecar.Port, messages, false, 0)
				if err != nil {
					return err
				}
				fmt.Println(response)
			} else {
				_, err = streamSidecar(sidecar.Port, messages)
				if err != nil {
					return err
				}
			}

			fmt.Fprintf(os.Stderr, "%s\n",
				utl.Gra(fmt.Sprintf("[%dms]", time.Since(t0).Milliseconds())))
			return nil
		},
	}

	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printProbeHelp()
	})

	cmd.Flags().StringVarP(&system, "system", "s", "", "Optional system prompt")
	cmd.Flags().BoolVarP(&noStream, "no-stream", "S", false, "Collect full response before printing")

	return cmd
}
