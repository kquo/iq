package main

import (
	"fmt"
	"path/filepath"

	"github.com/queone/governa-color"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"iq/internal/config"
	"iq/internal/kb"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "config",
		Aliases:      []string{"cfg"},
		Short:        "Inspect KB configuration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow()
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printConfigHelp()
	})
	cmd.AddCommand(newConfigShowCmd())
	return cmd
}

func newConfigShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show",
		Short:        "Show effective KB configuration",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow()
		},
	}
}

func printConfigHelp() {
	n := programName
	fmt.Printf("Inspect KB configuration.\n\n")
	fmt.Printf("%s\n", color.Whi9("USAGE"))
	fmt.Printf("  %s config [command]\n\n", n)
	fmt.Printf("%s\n", color.Whi9("COMMANDS"))
	fmt.Printf("  %-16s %s\n\n", "show", "Show effective configuration (default)")
	fmt.Printf("%s\n", color.Whi9("EXAMPLES"))
	fmt.Printf("  $ %s config\n", n)
	fmt.Printf("  $ %s config show\n", n)
}

// cfgField prints a "  label: value" line.
func cfgField(label, value string) {
	fmt.Printf("  %-28s %s\n", label+":", value)
}

func runConfigShow() error {
	dir, err := kbDir()
	if err != nil {
		return err
	}
	cfgPath := filepath.Join(dir, "config.yaml")

	cfg, err := loadKBConfig()
	if err != nil {
		return err
	}

	fmt.Printf("%s\n", color.Whi9("KB CONFIG"))
	cfgField("path", cfgPath)
	cfgField("embed_model", config.EmbedModel(cfg))

	// Inference params — marshal to map to iterate only set fields.
	raw, _ := yaml.Marshal(cfg)
	var flat map[string]any
	_ = yaml.Unmarshal(raw, &flat)

	paramFields := []string{"repetition_penalty", "temperature", "max_tokens",
		"top_p", "min_p", "top_k", "stop", "seed"}
	any := false
	for _, f := range paramFields {
		if _, ok := flat[f]; ok {
			any = true
			break
		}
	}
	if any {
		fmt.Printf("\n%s\n", color.Whi9("INFERENCE PARAMS"))
		for _, f := range paramFields {
			if v, ok := flat[f]; ok {
				cfgField(f, fmt.Sprintf("%v", v))
			}
		}
	}

	// Pool models (if any configured in kb config).
	if len(cfg.Models) > 0 {
		fmt.Printf("\n%s\n", color.Whi9("MODELS"))
		for _, me := range cfg.Models {
			cfgField("  id", color.Grn5(me.ID))
			if me.ContextWindow > 0 {
				cfgField("  context_window", fmt.Sprintf("%d  (model override)", me.ContextWindow))
			}
		}
	}

	// KB index summary.
	fmt.Printf("\n%s\n", color.Whi9("KNOWLEDGE BASE"))
	idxPath, _ := kbIndexPath()
	cfgField("index", idxPath)
	if kbIndexExists() {
		idx, lErr := kb.LoadFrom(dir)
		if lErr == nil {
			total := 0
			for _, s := range idx.Sources {
				total += s.ChunkCount
			}
			cfgField("sources", fmt.Sprintf("%d", len(idx.Sources)))
			cfgField("chunks", fmt.Sprintf("%d", total))
		}
	} else {
		cfgField("sources", color.Gra5("<empty>"))
	}

	return nil
}
