package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/felipefuhr/ffreis-workflow-ai-standardizer/internal/config"
	"github.com/felipefuhr/ffreis-workflow-ai-standardizer/internal/llm"
	"github.com/felipefuhr/ffreis-workflow-ai-standardizer/internal/output"
	"github.com/felipefuhr/ffreis-workflow-ai-standardizer/internal/runner"
	"github.com/spf13/cobra"
)

func main() {
	if err := rootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "standardizer",
		Short: "AI-assisted repo documentation maintenance",
	}
	root.AddCommand(runCmd(), tasksCmd())
	return root
}

func runCmd() *cobra.Command {
	var (
		reposConfig string
		tasksDir    string
		outputDir   string
		taskFilter  string
		repoFilter  string
		model       string
		baseURL     string
		dryRun      bool
		localDir    string
		repoSlug    string
	)

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run configured tasks across repos",
		Long: `Run tasks in one of two modes:

  Central mode (default): reads config/repos.yaml and iterates all configured repos.
    standardizer run [--task <name>] [--repo owner/name]

  Local mode: operates on an already-cloned directory (for use inside GitHub Actions).
    standardizer run --local-dir <path> --repo-slug owner/name [--task <name>]`,
		RunE: func(cmd *cobra.Command, args []string) error {
			logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

			apiKey := firstNonEmpty(
				os.Getenv("LLM_API_KEY"),
				os.Getenv("ANTHROPIC_API_KEY"),
				os.Getenv("OPENAI_API_KEY"),
			)
			if apiKey == "" && !dryRun {
				return fmt.Errorf("no LLM API key: set LLM_API_KEY, ANTHROPIC_API_KEY, or OPENAI_API_KEY")
			}
			if baseURL == "" {
				baseURL = os.Getenv("LLM_BASE_URL")
			}
			if model == "" {
				model = firstNonEmpty(os.Getenv("LLM_MODEL"), "claude-sonnet-4-6")
			}

			opts := runner.Options{
				ReposConfig: reposConfig,
				TasksDir:    tasksDir,
				LLMConfig: llm.Config{
					BaseURL: baseURL, APIKey: apiKey, Model: model,
					MaxRetries: 3, Timeout: 120 * time.Second,
				},
				GHToken:    os.Getenv("GH_TOKEN"),
				OutputDir:  outputDir,
				DryRun:     dryRun,
				TaskFilter: taskFilter,
				RepoFilter: repoFilter,
				LocalDir:   localDir,
				RepoSlug:   repoSlug,
				Logger:     logger,
			}

			start := time.Now()
			results, err := runner.Run(context.Background(), opts)
			if err != nil {
				return err
			}

			sum := output.RunSummary{StartedAt: start, FinishedAt: time.Now()}
			for _, r := range results {
				sum.Results = append(sum.Results, output.RepoResult{
					Repo: r.Repo, Task: r.Task, Status: r.Status, Detail: r.Detail,
				})
			}
			if outputDir != "" {
				if err := output.WriteSummary(outputDir, sum); err != nil {
					logger.Warn("write summary failed", "error", err)
				}
			}

			fmt.Println("\n── Run Summary ──────────────────────────────────")
			for _, r := range results {
				fmt.Printf("  %-40s  %-20s  %-12s  %s\n", r.Repo, r.Task, r.Status, r.Detail)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&reposConfig, "repos-config", "config/repos.yaml", "path to repos.yaml (central mode)")
	cmd.Flags().StringVar(&tasksDir, "tasks-dir", "tasks", "path to tasks directory")
	cmd.Flags().StringVar(&outputDir, "output-dir", "", "write summary JSON here")
	cmd.Flags().StringVar(&taskFilter, "task", "", "run only this task")
	cmd.Flags().StringVar(&repoFilter, "repo", "", "filter to one repo slug (central mode)")
	cmd.Flags().StringVar(&model, "model", "", "model override (default: claude-sonnet-4-6)")
	cmd.Flags().StringVar(&baseURL, "base-url", "", "LLM base URL (any OpenAI-compatible endpoint)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print rendered prompts, skip API calls and git writes")
	cmd.Flags().StringVar(&localDir, "local-dir", "", "local repo directory to use instead of cloning (local mode)")
	cmd.Flags().StringVar(&repoSlug, "repo-slug", "", "owner/name of the local repo (required with --local-dir)")

	return cmd
}

func tasksCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "tasks", Short: "Manage task configurations"}

	var tasksDir string

	list := &cobra.Command{
		Use:   "list",
		Short: "List all configured tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := os.ReadDir(tasksDir)
			if err != nil {
				return err
			}
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
					continue
				}
				tc, err := config.LoadTask(filepath.Join(tasksDir, e.Name()))
				if err != nil {
					fmt.Printf("  %-30s  ERROR: %v\n", e.Name(), err)
					continue
				}
				fmt.Printf("  %-20s  %-40s  output=%-10s  model=%s\n",
					tc.Name, tc.Description, tc.Output.Type, tc.Model)
			}
			return nil
		},
	}

	validate := &cobra.Command{
		Use:   "validate",
		Short: "Validate all task configs",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := os.ReadDir(tasksDir)
			if err != nil {
				return err
			}
			ok := true
			for _, e := range entries {
				if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
					continue
				}
				if _, err := config.LoadTask(filepath.Join(tasksDir, e.Name())); err != nil {
					fmt.Fprintf(os.Stderr, "FAIL %s: %v\n", e.Name(), err)
					ok = false
				} else {
					fmt.Printf("OK   %s\n", e.Name())
				}
			}
			if !ok {
				return fmt.Errorf("validation failed")
			}
			return nil
		},
	}

	for _, sub := range []*cobra.Command{list, validate} {
		sub.Flags().StringVar(&tasksDir, "tasks-dir", "tasks", "path to tasks directory")
		cmd.AddCommand(sub)
	}
	return cmd
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
