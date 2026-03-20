package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"iq/internal/color"
	"iq/internal/config"
	"iq/internal/embed"
	"iq/internal/kb"
)

func newKbIngestCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "ingest <path>",
		Aliases:      []string{"in"},
		Short:        "Ingest a file or directory into the knowledge base",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			idxPath, err := kbIndexPath()
			if err != nil {
				return err
			}
			return kb.IngestInto(args[0], idxPath)
		},
	}
}

func newKbListCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "list",
		Short:        "Show indexed sources",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := kbDir()
			if err != nil {
				return err
			}
			idx, err := kb.LoadFrom(dir)
			if err != nil {
				return err
			}
			if len(idx.Sources) == 0 {
				fmt.Printf("%s\n", color.Gra("knowledge base is empty — run: kb ingest <path>"))
				return nil
			}
			idxPath, _ := kbIndexPath()
			total := 0
			for _, s := range idx.Sources {
				total += s.ChunkCount
			}
			fmt.Printf("%-12s %s\n\n", "KB", idxPath)
			fmt.Printf("%-50s  %6s  %6s  %s\n", "SOURCE", "FILES", "CHUNKS", "INGESTED")
			for _, s := range idx.Sources {
				t, _ := time.Parse(time.RFC3339, s.IngestedAt)
				ingested := ""
				if !t.IsZero() {
					ingested = t.Format("2006-01-02 15:04")
				}
				fmt.Printf("%-50s  %6d  %6d  %s\n",
					s.Path, s.FileCount, s.ChunkCount, color.Gra(ingested))
			}
			fmt.Printf("\n%-50s  %6s  %6d\n", "TOTAL", "", total)
			return nil
		},
	}
}

func newKbSearchCmd() *cobra.Command {
	var topK int
	cmd := &cobra.Command{
		Use:          "search <query>",
		Short:        "Run a raw similarity search against the knowledge base",
		SilenceUsage: true,
		Args:         argsUsage(cobra.MinimumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !kbIndexExists() {
				return fmt.Errorf("knowledge base is empty — run: kb ingest <path>")
			}
			if !embed.SidecarAlive() {
				return fmt.Errorf("embed sidecar not running — run: kb start")
			}
			query := strings.Join(args, " ")

			dir, err := kbDir()
			if err != nil {
				return err
			}
			idx, err := kb.LoadFrom(dir)
			if err != nil {
				return err
			}

			vecs, err := embed.Texts(context.Background(), []string{query}, "query")
			if err != nil {
				return err
			}
			qvec := vecs[0]
			keywords := kb.ExtractKeywords(query)
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
			if topK < len(results) {
				results = results[:topK]
			}
			if len(results) == 0 {
				fmt.Printf("%s\n", color.Gra("no results"))
				return nil
			}
			cfg, _ := loadKBConfig()
			kbMinScore := config.KBMinScore(cfg)
			fmt.Printf("%s threshold:%.2f\n\n", color.Gra("kb search —"), kbMinScore)
			for _, r := range results {
				willInject := r.Score >= kbMinScore
				scoreStr := fmt.Sprintf("score:%.4f", r.Score)
				if !willInject {
					scoreStr = color.Gra(scoreStr + "  (below threshold — will not inject)")
				}
				labelStr := ""
				if r.Chunk.Label != "" {
					labelStr = "  [" + r.Chunk.Label + "]"
				}
				header := fmt.Sprintf("%s%s  %s  lines %d–%d",
					r.Chunk.Source, labelStr, scoreStr, r.Chunk.LineStart, r.Chunk.LineEnd)
				if willInject {
					fmt.Printf("%s\n", color.Whi(header))
				} else {
					fmt.Printf("%s\n", color.Gra(header))
				}
				lines := strings.SplitN(r.Chunk.Text, "\n", 4)
				preview := lines
				if len(lines) > 3 {
					preview = append(lines[:3], "...")
				}
				fmt.Printf("%s\n\n", color.Gra(strings.Join(preview, "\n")))
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&topK, "top", "k", kb.DefaultK, "Number of results to return")
	return cmd
}

func newKbRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "rm <path>",
		Short:        "Remove a source from the knowledge base",
		SilenceUsage: true,
		Args:         argsUsage(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			abs, err := filepath.Abs(args[0])
			if err != nil {
				return err
			}
			dir, err := kbDir()
			if err != nil {
				return err
			}
			idx, err := kb.LoadFrom(dir)
			if err != nil {
				return err
			}
			before := len(idx.Chunks)
			idx = kb.RemoveSource(idx, abs)
			removed := before - len(idx.Chunks)
			if removed == 0 {
				fmt.Printf("%s\n", color.Gra(fmt.Sprintf("%s not found in knowledge base", abs)))
				return nil
			}
			idxPath, err := kbIndexPath()
			if err != nil {
				return err
			}
			if err := kb.SaveTo(idxPath, idx); err != nil {
				return err
			}
			fmt.Printf("removed %s  (%d chunks)\n", color.Whi(abs), removed)
			return nil
		},
	}
}

func newKbClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "clear",
		Short:        "Wipe the entire knowledge base",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := kbIndexPath()
			if err != nil {
				return err
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return err
			}
			fmt.Printf("%s\n", color.Grn("knowledge base cleared"))
			return nil
		},
	}
}
