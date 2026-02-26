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
)

//go:embed roles_default.yaml
var defaultRolesYAML string

// ── Types ─────────────────────────────────────────────────────────────────────

type Role struct {
	Name          string `yaml:"name"`
	Category      string `yaml:"category"`
	Description   string `yaml:"description"`
	SystemPrompt  string `yaml:"system_prompt"`
	SuggestedTier string `yaml:"suggested_tier"`
	Model         string `yaml:"model"`
}

// ── Roles file path ───────────────────────────────────────────────────────────

func rolesPath() (string, error) {
	dir, err := iqConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "roles.yaml"), nil
}

// ── Load / save ───────────────────────────────────────────────────────────────

func loadRoles() ([]Role, error) {
	path, err := rolesPath()
	if err != nil {
		return nil, err
	}

	// Seed from defaults if file does not exist yet.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := saveRolesRaw([]byte(defaultRolesYAML), path); err != nil {
			return nil, fmt.Errorf("failed to seed roles file: %w", err)
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var roles []Role
	if err := yaml.Unmarshal(data, &roles); err != nil {
		return nil, fmt.Errorf("failed to parse roles.yaml: %w", err)
	}
	return roles, nil
}

func saveRoles(roles []Role) error {
	path, err := rolesPath()
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(roles)
	if err != nil {
		return err
	}
	return saveRolesRaw(data, path)
}

func saveRolesRaw(data []byte, path string) error {
	return os.WriteFile(path, data, 0644)
}

func loadDefaultRoles() ([]Role, error) {
	var roles []Role
	if err := yaml.Unmarshal([]byte(defaultRolesYAML), &roles); err != nil {
		return nil, err
	}
	return roles, nil
}

// ── Lookup helpers ────────────────────────────────────────────────────────────

func findRole(roles []Role, name string) (int, *Role) {
	for i := range roles {
		if roles[i].Name == name {
			return i, &roles[i]
		}
	}
	return -1, nil
}

// roleForModel returns the role name assigned to a model ID, or "<unassigned>".
func roleForModel(modelID string) string {
	roles, err := loadRoles()
	if err != nil {
		return "<unassigned>"
	}
	for _, r := range roles {
		if r.Model == modelID {
			return r.Name
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

func printRoleHelp() {
	n := program_name
	fmt.Printf("Work with IQ roles.\n\n")
	fmt.Printf("%s\n", utl.Whi2("USAGE"))
	fmt.Printf("  %s role <command> [flags]\n\n", n)
	fmt.Printf("%s\n", utl.Whi2("COMMANDS"))
	fmt.Printf("  %-10s %s\n", "list", "List all roles (alias: ls)")
	fmt.Printf("  %-10s %s\n", "show", "Show full details for a role")
	fmt.Printf("  %-10s %s\n", "add", "Add a new role")
	fmt.Printf("  %-10s %s\n", "edit", "Edit an existing role in $EDITOR")
	fmt.Printf("  %-10s %s\n", "rm", "Remove a role")
	fmt.Printf("  %-10s %s\n", "assign", "Assign a model to a role")
	fmt.Printf("  %-10s %s\n", "unassign", "Clear the model assignment for a role")
	fmt.Printf("  %-10s %s\n", "reset", "Reset all or one role to factory defaults")
	fmt.Printf("  %-10s %s\n\n", "sync", "Add new built-in roles without overwriting existing ones")
	fmt.Printf("%s\n", utl.Whi2("INHERITED FLAGS"))
	fmt.Printf("  %-30s %s\n\n", "-h, --help", "Show help for command")
	fmt.Printf("%s\n", utl.Whi2("EXAMPLES"))
	fmt.Printf("  $ %s role list\n", n)
	fmt.Printf("  $ %s role list --category reasoning\n", n)
	fmt.Printf("  $ %s role show math_reasoning\n", n)
	fmt.Printf("  $ %s role add my_custom_role\n", n)
	fmt.Printf("  $ %s role edit math_reasoning\n", n)
	fmt.Printf("  $ %s role assign math_reasoning mlx-community/gemma-3-1b-it-4bit\n", n)
	fmt.Printf("  $ %s role unassign math_reasoning\n", n)
	fmt.Printf("  $ %s role rm my_custom_role\n", n)
	fmt.Printf("  $ %s role reset\n", n)
	fmt.Printf("  $ %s role reset math_reasoning\n", n)
	fmt.Printf("  $ %s role sync\n\n", n)
}

// ── Root role command ─────────────────────────────────────────────────────────

func newRoleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "role",
		Short:        "Work with IQ roles",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			printRoleHelp()
			return nil
		},
	}
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		printRoleHelp()
	})

	cmd.AddCommand(
		newRoleListCmd(),
		newRoleShowCmd(),
		newRoleAddCmd(),
		newRoleEditCmd(),
		newRoleRmCmd(),
		newRoleAssignCmd(),
		newRoleUnassignCmd(),
		newRoleResetCmd(),
		newRoleSyncCmd(),
	)
	return cmd
}

// ── list / ls ─────────────────────────────────────────────────────────────────

func newRoleListCmd() *cobra.Command {
	var category string

	cmd := &cobra.Command{
		Use:          "list",
		Aliases:      []string{"ls"},
		Short:        "List all roles",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			roles, err := loadRoles()
			if err != nil {
				return err
			}

			// Collect and sort categories.
			catSet := map[string]bool{}
			for _, r := range roles {
				catSet[r.Category] = true
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
				for _, r := range roles {
					if r.Category != cat {
						continue
					}
					model := "<unassigned>"
					if r.Model != "" {
						model = r.Model
					}
					fmt.Printf("  %-38s  %-20s  %s\n", r.Name, model, r.Description)
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

func newRoleShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "show <name>",
		Short:        "Show full details for a role",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			roles, err := loadRoles()
			if err != nil {
				return err
			}
			_, role := findRole(roles, args[0])
			if role == nil {
				return fmt.Errorf("role %q not found", args[0])
			}

			model := "<unassigned>"
			if role.Model != "" {
				model = role.Model
			}
			fmt.Printf("%-16s %s\n", "NAME", role.Name)
			fmt.Printf("%-16s %s\n", "CATEGORY", role.Category)
			fmt.Printf("%-16s %s\n", "DESCRIPTION", role.Description)
			fmt.Printf("%-16s %s\n", "SUGGESTED TIER", role.SuggestedTier)
			fmt.Printf("%-16s %s\n", "MODEL", model)
			fmt.Printf("%-16s\n%s\n", "SYSTEM PROMPT", indentBlock(role.SystemPrompt, "  "))
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

func newRoleAddCmd() *cobra.Command {
	var category string
	var description string
	var tier string

	cmd := &cobra.Command{
		Use:          "add <name>",
		Short:        "Add a new role",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			roles, err := loadRoles()
			if err != nil {
				return err
			}
			if _, existing := findRole(roles, name); existing != nil {
				return fmt.Errorf("role %q already exists; use 'iq role edit %s' to modify it", name, name)
			}

			// Write a template to a temp file and open in $EDITOR.
			tmp, err := os.CreateTemp("", "iq-role-*.yaml")
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
  You are a ... (describe the role's behaviour here)
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
			var newRole Role
			if err := yaml.Unmarshal(data, &newRole); err != nil {
				return fmt.Errorf("failed to parse edited role: %w", err)
			}
			if newRole.Name == "" {
				return fmt.Errorf("role name is required")
			}
			if newRole.Category == "" {
				return fmt.Errorf("role category is required")
			}

			roles = append(roles, newRole)
			if err := saveRoles(roles); err != nil {
				return err
			}
			fmt.Printf("Added role %q\n", newRole.Name)
			return nil
		},
	}

	cmd.Flags().StringVarP(&category, "category", "c", "", "Category for the new role")
	cmd.Flags().StringVarP(&description, "description", "d", "", "Short description")
	cmd.Flags().StringVarP(&tier, "tier", "t", "balanced", "Suggested model tier")
	return cmd
}

// ── edit ──────────────────────────────────────────────────────────────────────

func newRoleEditCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "edit <name>",
		Short:        "Edit an existing role in $EDITOR",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			roles, err := loadRoles()
			if err != nil {
				return err
			}
			idx, role := findRole(roles, args[0])
			if role == nil {
				return fmt.Errorf("role %q not found", args[0])
			}

			// Serialize just this role (without model — managed via assign).
			editRole := Role{
				Name:          role.Name,
				Category:      role.Category,
				Description:   role.Description,
				SystemPrompt:  role.SystemPrompt,
				SuggestedTier: role.SuggestedTier,
			}
			data, err := yaml.Marshal(editRole)
			if err != nil {
				return err
			}

			tmp, err := os.CreateTemp("", "iq-role-*.yaml")
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
			var updatedRole Role
			if err := yaml.Unmarshal(updated, &updatedRole); err != nil {
				return fmt.Errorf("failed to parse edited role: %w", err)
			}
			// Preserve existing model assignment.
			updatedRole.Model = role.Model
			roles[idx] = updatedRole

			if err := saveRoles(roles); err != nil {
				return err
			}
			fmt.Printf("Updated role %q\n", updatedRole.Name)
			return nil
		},
	}
}

// ── rm ────────────────────────────────────────────────────────────────────────

func newRoleRmCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:          "rm <name>",
		Short:        "Remove a role",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			roles, err := loadRoles()
			if err != nil {
				return err
			}
			idx, role := findRole(roles, args[0])
			if role == nil {
				return fmt.Errorf("role %q not found", args[0])
			}
			if role.Model != "" && !force {
				return fmt.Errorf("role %q has model %q assigned; use --force to remove anyway", role.Name, role.Model)
			}

			roles = append(roles[:idx], roles[idx+1:]...)
			if err := saveRoles(roles); err != nil {
				return err
			}
			fmt.Printf("Removed role %q\n", args[0])
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Remove even if a model is assigned")
	return cmd
}

// ── assign ────────────────────────────────────────────────────────────────────

func newRoleAssignCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "assign <name> <model>",
		Short:        "Assign a model to a role",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			roleName, modelID := args[0], args[1]
			roles, err := loadRoles()
			if err != nil {
				return err
			}
			idx, role := findRole(roles, roleName)
			if role == nil {
				return fmt.Errorf("role %q not found", roleName)
			}

			// Warn if another role already has this model.
			for _, r := range roles {
				if r.Model == modelID && r.Name != roleName {
					fmt.Fprintf(os.Stderr, "%s\n", utl.Gra(
						fmt.Sprintf("warning: model %q is already assigned to role %q", modelID, r.Name)))
				}
			}

			roles[idx].Model = modelID
			if err := saveRoles(roles); err != nil {
				return err
			}
			fmt.Printf("Assigned %s → %s\n", modelID, roleName)
			return nil
		},
	}
}

// ── unassign ──────────────────────────────────────────────────────────────────

func newRoleUnassignCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "unassign <name>",
		Short:        "Clear the model assignment for a role",
		SilenceUsage: true,
		Args:         cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			roles, err := loadRoles()
			if err != nil {
				return err
			}
			idx, role := findRole(roles, args[0])
			if role == nil {
				return fmt.Errorf("role %q not found", args[0])
			}
			if role.Model == "" {
				fmt.Printf("Role %q has no model assigned.\n", args[0])
				return nil
			}
			prev := role.Model
			roles[idx].Model = ""
			if err := saveRoles(roles); err != nil {
				return err
			}
			fmt.Printf("Unassigned %s from %s\n", prev, args[0])
			return nil
		},
	}
}

// ── reset ─────────────────────────────────────────────────────────────────────

func newRoleResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "reset [name]",
		Short:        "Reset all or one role to factory defaults",
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			defaults, err := loadDefaultRoles()
			if err != nil {
				return err
			}

			// Single role reset.
			if len(args) == 1 {
				name := args[0]
				_, defaultRole := findRole(defaults, name)
				if defaultRole == nil {
					return fmt.Errorf("role %q is not a built-in role and cannot be reset", name)
				}

				roles, err := loadRoles()
				if err != nil {
					return err
				}
				idx, existing := findRole(roles, name)
				if existing != nil && existing.Model != "" {
					fmt.Printf("Warning: role %q has model %q assigned — assignment will be cleared.\n",
						name, existing.Model)
					fmt.Print("Proceed? [y/N] ")
					var resp string
					fmt.Scanln(&resp)
					if strings.ToLower(strings.TrimSpace(resp)) != "y" {
						fmt.Println("Aborted.")
						return nil
					}
				}

				restored := *defaultRole
				if idx >= 0 {
					roles[idx] = restored
				} else {
					roles = append(roles, restored)
				}
				if err := saveRoles(roles); err != nil {
					return err
				}
				fmt.Printf("Reset role %q to factory default.\n", name)
				return nil
			}

			// Full reset — show exactly what will be lost.
			roles, err := loadRoles()
			if err != nil {
				return err
			}

			defaultNames := map[string]bool{}
			for _, r := range defaults {
				defaultNames[r.Name] = true
			}

			var assigned []Role
			var custom []Role
			for _, r := range roles {
				if r.Model != "" {
					assigned = append(assigned, r)
				}
				if !defaultNames[r.Name] {
					custom = append(custom, r)
				}
			}

			fmt.Printf("%s\n", utl.Gra("WARNING: This will reset ALL roles to factory defaults."))
			if len(assigned) > 0 {
				fmt.Printf("\nThe following model assignments will be cleared:\n")
				for _, r := range assigned {
					fmt.Printf("  %-38s → %s\n", r.Name, r.Model)
				}
			}
			if len(custom) > 0 {
				fmt.Printf("\nThe following custom roles will be deleted:\n")
				for _, r := range custom {
					fmt.Printf("  %s\n", r.Name)
				}
			}

			path, _ := rolesPath()
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

			if err := saveRolesRaw([]byte(defaultRolesYAML), path); err != nil {
				return fmt.Errorf("failed to write defaults: %w", err)
			}
			fmt.Println("Roles reset to factory defaults.")
			return nil
		},
	}
}

// ── sync ──────────────────────────────────────────────────────────────────────

func newRoleSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "sync",
		Short:        "Add new built-in roles without overwriting existing ones",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			defaults, err := loadDefaultRoles()
			if err != nil {
				return err
			}
			roles, err := loadRoles()
			if err != nil {
				return err
			}

			existing := map[string]bool{}
			for _, r := range roles {
				existing[r.Name] = true
			}

			var added []string
			for _, d := range defaults {
				if !existing[d.Name] {
					roles = append(roles, d)
					added = append(added, d.Name)
				}
			}

			if len(added) == 0 {
				fmt.Println("Already up to date.")
				return nil
			}

			if err := saveRoles(roles); err != nil {
				return err
			}
			fmt.Printf("Added %d new role(s):\n", len(added))
			for _, name := range added {
				fmt.Printf("  %s\n", name)
			}
			return nil
		},
	}
}
