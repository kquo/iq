package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"iq/internal/color"
	"iq/internal/config"
	"iq/internal/embed"
	"iq/internal/kb"
	"iq/internal/sidecar"
)

const kbSystemPrompt = "Answer using only the provided context. If the context does not contain the answer, say so."

// askOpts holds flags for the ask command and the root inline-ask path.
type askOpts struct {
	query string // populated by root RunE before calling runAsk
	model string
	noKB  bool
	topK  int
}

// addAskFlags binds ask flags onto cmd, writing into opts.
func addAskFlags(cmd *cobra.Command, opts *askOpts) {
	cmd.Flags().StringVar(&opts.model, "model", "", "Override inference model (must be running)")
	cmd.Flags().BoolVarP(&opts.noKB, "no-kb", "K", false, "Skip KB retrieval, run pure inference")
	cmd.Flags().IntVarP(&opts.topK, "top-k", "k", kb.DefaultK, "Number of KB chunks to retrieve")
}

func newAskCmd() *cobra.Command {
	var opts askOpts
	cmd := &cobra.Command{
		Use:          "ask <query>",
		Short:        "Ask a question using KB-grounded inference",
		SilenceUsage: true,
		Args:         argsUsage(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()
			opts.query = strings.Join(args, " ")
			return runAsk(ctx, opts)
		},
	}
	addAskFlags(cmd, &opts)
	return cmd
}

// loadKBConfig loads config from ~/.config/kb/config.yaml.
func loadKBConfig() (*config.Config, error) {
	dir, err := config.DirFor("kb")
	if err != nil {
		return nil, err
	}
	return config.LoadAt(filepath.Join(dir, "config.yaml"), nil)
}

// kbDir returns the kb config directory (~/.config/kb/).
func kbDir() (string, error) {
	return config.DirFor("kb")
}

// kbIndexPath returns the path to ~/.config/kb/kb.json.
func kbIndexPath() (string, error) {
	dir, err := kbDir()
	if err != nil {
		return "", err
	}
	return kb.PathFor(dir), nil
}

// kbIndexExists reports whether ~/.config/kb/kb.json is non-empty.
func kbIndexExists() bool {
	path, err := kbIndexPath()
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}

// runAsk executes the KB-grounded inference pipeline.
func runAsk(ctx context.Context, opts askOpts) error {
	cfg, err := loadKBConfig()
	if err != nil {
		return err
	}

	// Resolve inference sidecar.
	var port int
	var modelID string
	if opts.model != "" {
		sc, sErr := sidecar.ReadState(opts.model)
		if sErr != nil || sc == nil || !sidecar.PidAlive(sc.PID) {
			return fmt.Errorf("--model %s: not running", opts.model)
		}
		port = sc.Port
		modelID = sc.Model
	} else {
		sc, sErr := pickAnySidecar()
		if sErr != nil {
			return fmt.Errorf("no inference sidecar running — run 'kb start <model>'")
		}
		port = sc.Port
		modelID = sc.Model
	}

	ip := config.ResolveInferParams(cfg, modelID)

	// Assemble messages with optional KB context.
	var messages []config.Message
	if !opts.noKB && kbIndexExists() {
		if !embed.SidecarAlive() {
			return fmt.Errorf("embed sidecar not running — run 'kb start'")
		}
		kbCtx, kbErr := searchKB(ctx, cfg, opts.query, opts.topK)
		if kbErr != nil {
			fmt.Fprintf(os.Stderr, "%s\n", color.Gra("kb search error: "+kbErr.Error()))
		}
		if kbCtx != "" {
			messages = append(messages,
				config.Message{Role: "system", Content: kbSystemPrompt},
				config.Message{Role: "user", Content: kbCtx + "\n\n" + opts.query},
			)
		}
	}
	if len(messages) == 0 {
		messages = append(messages,
			config.Message{Role: "system", Content: "You are a helpful assistant."},
			config.Message{Role: "user", Content: opts.query},
		)
	}

	_, err = sidecar.Stream(ctx, port, messages, ip)
	return err
}

// searchKB embeds the query and searches ~/.config/kb/kb.json.
func searchKB(ctx context.Context, cfg *config.Config, query string, topK int) (string, error) {
	dir, err := kbDir()
	if err != nil {
		return "", err
	}
	idx, err := kb.LoadFrom(dir)
	if err != nil {
		return "", err
	}
	if len(idx.Chunks) == 0 {
		return "", nil
	}

	vecs, err := embed.Texts(ctx, []string{query}, "query")
	if err != nil {
		return "", fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) == 0 {
		return "", fmt.Errorf("empty embedding response")
	}
	qvec := vecs[0]

	keywords := kb.ExtractKeywords(query)
	minScore := config.KBMinScore(cfg)

	results := make([]kb.Result, 0, len(idx.Chunks))
	for _, c := range idx.Chunks {
		if len(c.Embedding) == 0 {
			continue
		}
		score := embed.CosineSimilarity(qvec, c.Embedding)
		score += kb.KeywordBoost(c.Text, c.Label, keywords)
		results = append(results, kb.Result{Chunk: c, Score: score})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})
	filtered := results[:0]
	for _, r := range results {
		if r.Score >= minScore {
			filtered = append(filtered, r)
		}
	}
	results = filtered
	if topK < len(results) {
		results = results[:topK]
	}
	return kb.Context(results), nil
}
