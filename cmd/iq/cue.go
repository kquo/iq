package main

import (
	"bufio"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/queone/utl"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
	"iq/internal/config"
)

//go:embed cues_default.yaml
var defaultCuesYAML string

// ── Types ─────────────────────────────────────────────────────────────────────

type Cue struct {
	Name          string `yaml:"name"`
	Category      string `yaml:"category"`
	Description   string `yaml:"description"`
	SystemPrompt  string `yaml:"system_prompt"`
	SuggestedTier string `yaml:"suggested_tier"`
	Model         string `yaml:"model"`
}

// ── Roles file path ───────────────────────────────────────────────────────────

func cuesPath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "cues.yaml"), nil
}

// ── Load / save ───────────────────────────────────────────────────────────────

func loadCues() ([]Cue, error) {
	path, err := cuesPath()
	if err != nil {
		return nil, err
	}

	// Seed from defaults if file does not exist yet.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := saveCuesRaw([]byte(defaultCuesYAML), path); err != nil {
			return nil, fmt.Errorf("failed to seed cues file: %w", err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cues []Cue
	if err := yaml.Unmarshal(data, &cues); err != nil {
		return nil, fmt.Errorf("failed to parse cues.yaml: %w", err)
	}
	return cues, nil
}

func saveCues(cues []Cue) error {
	path, err := cuesPath()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(cues)
	if err != nil {
		return err
	}
	return saveCuesRaw(data, path)
}

func saveCuesRaw(data []byte, path string) error {
	return os.WriteFile(path, data, 0644)
}

func loadDefaultCues() ([]Cue, error) {
	var cues []Cue
	if err := yaml.Unmarshal([]byte(defaultCuesYAML), &cues); err != nil {
		return nil, err
	}
	return cues, nil
}

// ── Lookup helpers ────────────────────────────────────────────────────────────

func findCue(cues []Cue, name string) (int, *Cue) {
	for i := range cues {
		if cues[i].Name == name {
			return i, &cues[i]
		}
	}
	return -1, nil
}

// cueForModel returns the cue name assigned to a model ID, or "<unassigned>".
func cueForModel(modelID string) string {
	cues, err := loadCues()
	if err != nil {
		return "<unassigned>"
	}
	for _, c := range cues {
		if c.Model == modelID {
			return c.Name
		}
	}
	return "<unassigned>"
}

// ── $EDITOR helper ────────────────────────────────────────────────────────────

func openInEditor(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command("/bin/sh", "-c", editor+" "+shellescape(path))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ── Help ──────────────────────────────────────────────────────────────────────

func printCueHelp() {
	n := program_name
	fmt.Printf("Work with IQ cues.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s cue <command> [flags]\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-10s %s\n", "list", "List all cues (alias: ls)")
	fmt.Printf("  %-10s %s\n", "show", "Show full details for a cue")
	fmt.Printf("  %-10s %s\n", "add", "Add a new cue")
	fmt.Printf("  %-10s %s\n", "edit", "Edit an existing cue in $EDITOR")
	fmt.Printf("  %-10s %s\n", "rm", "Remove a cue")
	fmt.Printf("  %-10s %s\n", "assign", "Assign a model to a cue")
	fmt.Printf("  %-10s %s\n", "unassign", "Clear the model assignment for a cue")
	fmt.Printf("  %-10s %s\n", "reset", "Reset all or one cue to factory defaults")
	fmt.Printf("  %-10s %s\n\n", "sync", "Add new built-in cues without overwriting existing ones")
	fmt.Printf("%s\n", utl.Whi2("INHERITED FLAGS"))
	fmt.Printf("  %-30s %s\n\n", "-h, --help", "Show help for command")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s cue list\n", n)
	fmt.Printf("  $ %s cue list --category reasoning\n", n)
	fmt.Printf("  $ %s cue show math\n", n)
	fmt.Printf("  $ %s cue add my_custom_cue\n", n)
	fmt.Printf("  $ %s cue edit math\n", n)
	fmt.Printf("  $ %s cue assign math mlx-community/gemma-3-1b-it-4bit\n", n)
	fmt.Printf("  $ %s cue unassign math\n", n)
	fmt.Printf("  $ %s cue rm my_custom_cue\n", n)
	fmt.Printf("  $ %s cue reset\n", n)
	fmt.Printf("  $ %s cue reset math\n", n)
	fmt.Printf("  $ %s cue sync\n\n", n)
}

// ── Root cue command ──────────────────────────────────────────────────────────

func newCueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "cue",
		Short:        "Work with IQ cues",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printCueHelp()
			return nil
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printCueHelp()
	})

	cmd.AddCommand(
		newCueListCmd(),
		newCueShowCmd(),
		newCueAddCmd(),
		newCueEditCmd(),
		newCueRmCmd(),
		newCueAssignCmd(),
		newCueUnassignCmd(),
		newCueResetCmd(),
		newCueSyncCmd(),
	)
	return cmd
}

// ── list / ls ─────────────────────────────────────────────────────────────────

func newCueListCmd() *cobra.Command {
	var category string

	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List all cues",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			cues, err := loadCues()
			if err != nil {
				return err
			}

			// Collect and sort categories.
			catSet := map[string]bool{}
			for _, c := range cues {
				catSet[c.Category] = true
			}
			cats := make([]string, 0, len(catSet))
			for c := range catSet {
				cats = append(cats, c)
			}
			sort.Strings(cats)

			if category != "" {
				cats = []string{category}
			}

			for _, cat := range cats {
				fmt.Printf("%s\n", utl.Whi2(cat))
				for _, c := range cues {
					if c.Category != cat {
						continue
					}
					model := "<unassigned>"
					if c.Model != "" {
						model = c.Model
					}
					fmt.Printf("  %-38s  %-20s  %s\n", c.Name, model, c.Description)
				}
				fmt.Println()
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&category, "category", "c", "", "Filter by category")
	return cmd
}

// ── show ──────────────────────────────────────────────────────────────────────

func newCueShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show <name>",
		Short:        "Show full details for a cue",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			cues, err := loadCues()
			if err != nil {
				return err
			}
			_, cue := findCue(cues, args[0])
			if cue == nil {
				return fmt.Errorf("cue %q not found", args[0])
			}

			model := "<unassigned>"
			if cue.Model != "" {
				model = cue.Model
			}
			fmt.Printf("%-16s %s\n", "NAME", cue.Name)
			fmt.Printf("%-16s %s\n", "CATEGORY", cue.Category)
			fmt.Printf("%-16s %s\n", "DESCRIPTION", cue.Description)
			fmt.Printf("%-16s %s\n", "SUGGESTED TIER", cue.SuggestedTier)
			fmt.Printf("%-16s %s\n", "MODEL", model)
			fmt.Printf("%-16s\n%s\n", "SYSTEM PROMPT", indentBlock(cue.SystemPrompt, "  "))
			return nil
		},
	}
}

// indentBlock indents every line of a multiline string.
func indentBlock(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// ── add ───────────────────────────────────────────────────────────────────────

func newCueAddCmd() *cobra.Command {
	var category string
	var description string
	var tier string

	cmd := &cobra.Command{
		Use:          "add <name>",
		Short:        "Add a new cue",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cues, err := loadCues()
			if err != nil {
				return err
			}
			if _, existing := findCue(cues, name); existing != nil {
				return fmt.Errorf("cue %q already exists; use 'iq cue edit %s' to modify it", name, name)
			}

			// Write a template to a temp file and open in $EDITOR.
			tmp, err := os.CreateTemp("", "iq-cue-*.yaml")
			if err != nil {
				return err
			}
			tmpPath := tmp.Name()
			defer os.Remove(tmpPath)

			template := fmt.Sprintf(`name: %s
category: %s
description: %s
suggested_tier: %s
system_prompt: |
  You are a ... (describe the cue's behaviour here)
`, name, category, description, tier)
			if _, err := tmp.WriteString(template); err != nil {
				return err
			}
			tmp.Close()

			if err := openInEditor(tmpPath); err != nil {
				return fmt.Errorf("editor failed: %w", err)
			}

			data, err := os.ReadFile(tmpPath)
			if err != nil {
				return err
			}
			var newCue Cue
			if err := yaml.Unmarshal(data, &newCue); err != nil {
				return fmt.Errorf("failed to parse edited cue: %w", err)
			}
			if newCue.Name == "" {
				return fmt.Errorf("cue name is required")
			}
			if newCue.Category == "" {
				return fmt.Errorf("cue category is required")
			}

			cues = append(cues, newCue)
			if err := saveCues(cues); err != nil {
				return err
			}
			invalidateCueEmbeddings()
			fmt.Printf("Added cue %q\n", newCue.Name)
			return nil
		},
	}

	cmd.Flags().StringVarP(&category, "category", "c", "", "Category for the new cue")
	cmd.Flags().StringVarP(&description, "description", "d", "", "Short description")
	cmd.Flags().StringVarP(&tier, "tier", "t", "balanced", "Suggested model tier")
	return cmd
}

// ── edit ──────────────────────────────────────────────────────────────────────

func newCueEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "edit <name>",
		Short:        "Edit an existing cue in $EDITOR",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			cues, err := loadCues()
			if err != nil {
				return err
			}
			idx, cue := findCue(cues, args[0])
			if cue == nil {
				return fmt.Errorf("cue %q not found", args[0])
			}

			// Serialize just this cue (without model — managed via assign).
			editCue := Cue{
				Name:          cue.Name,
				Category:      cue.Category,
				Description:   cue.Description,
				SystemPrompt:  cue.SystemPrompt,
				SuggestedTier: cue.SuggestedTier,
			}
			data, err := yaml.Marshal(editCue)
			if err != nil {
				return err
			}

			tmp, err := os.CreateTemp("", "iq-cue-*.yaml")
			if err != nil {
				return err
			}
			tmpPath := tmp.Name()
			defer os.Remove(tmpPath)

			if _, err := tmp.Write(data); err != nil {
				return err
			}
			tmp.Close()

			if err := openInEditor(tmpPath); err != nil {
				return fmt.Errorf("editor failed: %w", err)
			}

			updated, err := os.ReadFile(tmpPath)
			if err != nil {
				return err
			}
			var updatedCue Cue
			if err := yaml.Unmarshal(updated, &updatedCue); err != nil {
				return fmt.Errorf("failed to parse edited cue: %w", err)
			}
			// Preserve existing model assignment.
			updatedCue.Model = cue.Model
			cues[idx] = updatedCue

			if err := saveCues(cues); err != nil {
				return err
			}
			invalidateCueEmbeddings()
			fmt.Printf("Updated cue %q\n", updatedCue.Name)
			return nil
		},
	}
}

// ── rm ────────────────────────────────────────────────────────────────────────

func newCueRmCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:          "rm <name>",
		Short:        "Remove a cue",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			cues, err := loadCues()
			if err != nil {
				return err
			}
			idx, cue := findCue(cues, args[0])
			if cue == nil {
				return fmt.Errorf("cue %q not found", args[0])
			}
			if cue.Model != "" && !force {
				return fmt.Errorf("cue %q has model %q assigned; use --force to remove anyway", cue.Name, cue.Model)
			}

			cues = append(cues[:idx], cues[idx+1:]...)
			if err := saveCues(cues); err != nil {
				return err
			}
			invalidateCueEmbeddings()
			fmt.Printf("Removed cue %q\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Remove even if a model is assigned")
	return cmd
}

// ── assign ────────────────────────────────────────────────────────────────────

func newCueAssignCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "assign <name> <model>",
		Short:        "Assign a model to a cue",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			cueName, modelID := args[0], args[1]
			cues, err := loadCues()
			if err != nil {
				return err
			}
			idx, cue := findCue(cues, cueName)
			if cue == nil {
				return fmt.Errorf("cue %q not found", cueName)
			}

			// Warn if another cue already has this model.
			for _, c := range cues {
				if c.Model == modelID && c.Name != cueName {
					fmt.Fprintf(os.Stderr, "%s\n", utl.Gra(
						fmt.Sprintf("warning: model %q is already assigned to cue %q", modelID, c.Name)))
				}
			}

			cues[idx].Model = modelID
			if err := saveCues(cues); err != nil {
				return err
			}
			invalidateCueEmbeddings()
			fmt.Printf("Assigned %s → %s\n", modelID, cueName)
			return nil
		},
	}
}

// ── unassign ──────────────────────────────────────────────────────────────────

func newCueUnassignCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "unassign <name>",
		Short:        "Clear the model assignment for a cue",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			cues, err := loadCues()
			if err != nil {
				return err
			}
			idx, cue := findCue(cues, args[0])
			if cue == nil {
				return fmt.Errorf("cue %q not found", args[0])
			}
			if cue.Model == "" {
				fmt.Printf("Role %q has no model assigned.\n", args[0])
				return nil
			}
			prev := cue.Model
			cues[idx].Model = ""
			if err := saveCues(cues); err != nil {
				return err
			}
			fmt.Printf("Unassigned %s from %s\n", prev, args[0])
			return nil
		},
	}
}

// ── reset ─────────────────────────────────────────────────────────────────────

func newCueResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "reset [name]",
		Short:        "Reset all or one cue to factory defaults",
		SilenceUsage: true,
		Args:         argsUsage(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			defaults, err := loadDefaultCues()
			if err != nil {
				return err
			}

			// Single cue reset.
			if len(args) == 1 {
				name := args[0]
				_, defaultCue := findCue(defaults, name)
				if defaultCue == nil {
					return fmt.Errorf("cue %q is not a built-in cue and cannot be reset", name)
				}

				cues, err := loadCues()
				if err != nil {
					return err
				}
				idx, existing := findCue(cues, name)
				if existing != nil && existing.Model != "" {
					fmt.Printf("Warning: cue %q has model %q assigned — assignment will be cleared.\n",
						name, existing.Model)
					fmt.Print("Proceed? [y/N] ")
					var resp string
					fmt.Scanln(&resp)
					if strings.ToLower(strings.TrimSpace(resp)) != "y" {
						fmt.Println("Aborted.")
						return nil
					}
				}

				restored := *defaultCue
				if idx >= 0 {
					cues[idx] = restored
				} else {
					cues = append(cues, restored)
				}
				if err := saveCues(cues); err != nil {
					return err
				}
				invalidateCueEmbeddings()
				fmt.Printf("Reset cue %q to factory default.\n", name)
				return nil
			}

			// Full reset — show exactly what will be lost.
			cues, err := loadCues()
			if err != nil {
				return err
			}

			defaultNames := map[string]bool{}
			for _, r := range defaults {
				defaultNames[r.Name] = true
			}

			var assigned []Cue
			var custom []Cue
			for _, c := range cues {
				if c.Model != "" {
					assigned = append(assigned, c)
				}
				if !defaultNames[c.Name] {
					custom = append(custom, c)
				}
			}

			fmt.Printf("%s\n", utl.Gra("WARNING: This will reset ALL cues to factory defaults."))
			if len(assigned) > 0 {
				fmt.Printf("\nThe following model assignments will be cleared:\n")
				for _, r := range assigned {
					fmt.Printf("  %-38s → %s\n", r.Name, r.Model)
				}
			}
			if len(custom) > 0 {
				fmt.Printf("\nThe following custom cues will be deleted:\n")
				for _, r := range custom {
					fmt.Printf("  %s\n", r.Name)
				}
			}

			path, _ := cuesPath()
			fmt.Printf("\nA backup will be written to %s.bak\n", path)
			fmt.Printf("\nType \"reset\" to confirm, or anything else to abort: ")

			reader := bufio.NewReader(os.Stdin)
			resp, _ := reader.ReadString('\n')
			resp = strings.TrimSpace(resp)
			if resp != "reset" {
				fmt.Println("Aborted.")
				return nil
			}

			// Backup current file.
			if data, err := os.ReadFile(path); err == nil {
				os.WriteFile(path+".bak", data, 0644)
			}

			if err := saveCuesRaw([]byte(defaultCuesYAML), path); err != nil {
				return fmt.Errorf("failed to write defaults: %w", err)
			}
			invalidateCueEmbeddings()
			fmt.Println("Cues reset to factory defaults.")
			return nil
		},
	}
}

// ── sync ──────────────────────────────────────────────────────────────────────

func newCueSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "sync",
		Short:        "Add new built-in cues without overwriting existing ones",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			defaults, err := loadDefaultCues()
			if err != nil {
				return err
			}
			cues, err := loadCues()
			if err != nil {
				return err
			}

			existing := map[string]bool{}
			for _, c := range cues {
				existing[c.Name] = true
			}

			var added []string
			for _, d := range defaults {
				if !existing[d.Name] {
					cues = append(cues, d)
					added = append(added, d.Name)
				}
			}

			if len(added) == 0 {
				fmt.Println("Already up to date.")
				return nil
			}

			if err := saveCues(cues); err != nil {
				return err
			}
			invalidateCueEmbeddings()
			fmt.Printf("Added %d new cue(s):\n", len(added))
			for _, name := range added {
				fmt.Printf("  %s\n", name)
			}
			return nil
		},
	}
}
