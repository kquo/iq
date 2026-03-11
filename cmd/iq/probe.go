package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/queone/utl"
	"github.com/spf13/cobra"
	"iq/internal/config"
	"iq/internal/cue"
	"iq/internal/embed"
	"iq/internal/kb"
	"iq/internal/sidecar"
)

// ── Help ──────────────────────────────────────────────────────────────────────

func printProbeHelp() {
	n := program_name
	fmt.Printf("Send a raw message directly to a model sidecar, bypassing the IQ framework.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s pry <model|tier> [flags] <message>\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("FLAGS"))
	fmt.Printf("  %-30s %s\n", "-c, --cue <name>", "Use a cue's system prompt")
	fmt.Printf("  %-30s %s\n", "-s, --system <text>", "Use a literal system prompt")
	fmt.Printf("  %-30s %s\n\n", "-S, --no-stream", "Collect full response before printing")
	fmt.Printf("%s\n", utl.Whi2("INHERITED FLAGS"))
	fmt.Printf("  %-30s %s\n\n", "-h, --help", "Show help for command")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s pry fast \"what is 2+2?\"\n", n)
	fmt.Printf("  $ %s pry slow \"explain gradient descent\"\n", n)
	fmt.Printf("  $ %s pry mlx-community/SmolLM2-135M-Instruct-8bit \"hello\"\n", n)
	fmt.Printf("  $ %s pry fast \"respond in pirate speak\" -s \"You are a pirate.\"\n", n)
	fmt.Printf("  $ %s pry fast \"solve x^2 + 3x - 4\" -c math\n", n)
}

// ── Command ───────────────────────────────────────────────────────────────────

func newProbeCmd() *cobra.Command {
	var cueName string
	var system string
	var noStream bool
	var useKB bool

	cmd := &cobra.Command{
		Use:          "pry <model|tier> <message>",
		Aliases:      []string{"probe"},
		Short:        "Send a raw message directly to a model sidecar",
		SilenceUsage: true,
		Args:         argsUsage(cobra.MinimumNArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if cueName != "" && system != "" {
				return fmt.Errorf("--cue and --system are mutually exclusive — use one or the other")
			}

			target := args[0]
			message := strings.Join(args[1:], " ")

			// Resolve system prompt — from cue or literal.
			systemPrompt := system
			if cueName != "" {
				cues, err := cue.Load()
				if err != nil {
					return err
				}
				_, c := cue.Find(cues, cueName)
				if c == nil {
					return fmt.Errorf("cue %q not found", cueName)
				}
				systemPrompt = c.SystemPrompt
			}

			// KB retrieval — prepend context to system prompt.
			if useKB {
				if !kb.Exists() {
					fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("kb: knowledge base is empty — run: iq kb ingest <path>"))
				} else if !embed.SidecarAlive() {
					fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("kb: embed sidecar not running — run: iq start"))
				} else {
					results, kbErr := kb.Search(message, kb.DefaultK)
					if kbErr != nil {
						fmt.Fprintf(os.Stderr, "%s\n", utl.Gra("kb search error: "+kbErr.Error()))
					} else if len(results) > 0 {
						ctx := kb.Context(results)
						if systemPrompt != "" {
							systemPrompt = systemPrompt + "\n\n" + ctx
						} else {
							systemPrompt = ctx
						}
						fmt.Fprintf(os.Stderr, "%s\n", utl.Gra(fmt.Sprintf("[kb: %d chunks retrieved]", len(results))))
					}
				}
			}

			// Resolve sidecar — tier name or specific model ID.
			var sc *sidecar.State
			var err error
			switch target {
			case "fast", "slow":
				sc, err = pickSidecar(target, false)
				if err != nil {
					return err
				}
			default:
				// Try by slug first (e.g. state file keyed by model ID).
				sc, err = sidecar.ReadState(target)
				if err != nil {
					return err
				}
				// If not found by slug, scan all live states for a matching Model field.
				// This handles the embed sidecar whose state is keyed as "embed" but
				// whose Model field holds the full HF model ID.
				if sc == nil || !sidecar.PidAlive(sc.PID) {
					live, lErr := sidecar.AllLiveStates()
					if lErr == nil {
						for _, s := range live {
							if s.Model == target {
								sc = s
								break
							}
						}
					}
				}
				if sc == nil || !sidecar.PidAlive(sc.PID) {
					return fmt.Errorf("%s is not running — run 'iq start %s' first", target, target)
				}
				if sc.Tier == "embed" {
					return fmt.Errorf("%s is an embedding model — it does not support chat inference", target)
				}
			}

			// Print routing header in gray.
			cueTag := ""
			if cueName != "" {
				cueTag = "  cue:" + cueName
			}
			fmt.Fprintf(os.Stderr, "%s\n",
				utl.Gra(fmt.Sprintf("[%s  %s  :%d%s]",
					sc.Tier, sc.Model, sc.Port, cueTag)))

			// Build messages.
			var messages []config.Message
			if systemPrompt != "" {
				messages = append(messages, config.Message{Role: "system", Content: systemPrompt})
			}
			messages = append(messages, config.Message{Role: "user", Content: message})

			// Resolve inference parameters for this tier.
			probeCfg, _ := config.Load(nil)
			probeIP := config.ResolveInferParams(probeCfg, sc.Tier)

			// Infer and time it.
			t0 := time.Now()
			if noStream {
				response, err := sidecar.Call(sc.Port, messages, probeIP.MaxTokens, probeIP)
				if err != nil {
					return err
				}
				fmt.Println(response)
			} else {
				_, err = sidecar.Stream(sc.Port, messages, probeIP)
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

	cmd.Flags().StringVarP(&cueName, "cue", "c", "", "Use a cue's system prompt")
	cmd.Flags().StringVarP(&system, "system", "s", "", "Use a literal system prompt")
	cmd.Flags().BoolVarP(&noStream, "no-stream", "S", false, "Collect full response before printing")
	cmd.Flags().BoolVarP(&useKB, "kb", "k", false, "Retrieve knowledge base context for this probe")

	return cmd
}
