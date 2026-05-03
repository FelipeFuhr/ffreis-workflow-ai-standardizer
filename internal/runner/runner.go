package runner

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	gocontext "github.com/felipefuhr/ffreis-workflow-ai-standardizer/internal/context"
	"github.com/felipefuhr/ffreis-workflow-ai-standardizer/internal/config"
	"github.com/felipefuhr/ffreis-workflow-ai-standardizer/internal/llm"
	"github.com/felipefuhr/ffreis-workflow-ai-standardizer/internal/output"
	"github.com/felipefuhr/ffreis-workflow-ai-standardizer/internal/tmpl"
	"github.com/google/go-github/v62/github"
	"golang.org/x/oauth2"
)

// Options configures a run.
type Options struct {
	// Central mode: read repos from this config.
	ReposConfig string

	TasksDir  string
	LLMConfig llm.Config
	GHToken   string
	OutputDir string
	DryRun    bool

	// Filters (both modes).
	TaskFilter string
	RepoFilter string // owner/name; filters central mode, required for local mode

	// Local mode: use a pre-cloned directory instead of cloning.
	// When set, RepoSlug (or GITHUB_REPOSITORY) provides owner/name.
	LocalDir string
	RepoSlug string // owner/name, used when LocalDir is set

	Logger *slog.Logger
}

// Result is the outcome for one repo x task pair.
type Result struct {
	Repo   string
	Task   string
	Status string // skipped | pr_opened | no_changes | error
	Detail string
}

// Run executes all configured repo x task pairs and returns results.
func Run(ctx context.Context, opts Options) ([]Result, error) {
	// Load task configs.
	taskCfgs, err := loadTaskConfigs(opts.TasksDir)
	if err != nil {
		return nil, err
	}

	ghClient := buildGHClient(ctx, opts.GHToken)
	llmClient := llm.New(opts.LLMConfig, opts.Logger)
	prHandler := &output.PRHandler{GHClient: ghClient, Logger: opts.Logger}

	// Local mode: single repo, pre-cloned directory.
	if opts.LocalDir != "" {
		return runLocalMode(ctx, opts, taskCfgs, llmClient, prHandler)
	}

	// Central mode: iterate repos from config.
	reposCfg, err := config.LoadRepos(opts.ReposConfig)
	if err != nil {
		return nil, fmt.Errorf("load repos config: %w", err)
	}
	return runCentralMode(ctx, opts, reposCfg, taskCfgs, llmClient, prHandler)
}

func runLocalMode(
	ctx context.Context, opts Options, taskCfgs map[string]config.TaskConfig,
	llmClient *llm.Client, prHandler *output.PRHandler,
) ([]Result, error) {
	slug := opts.RepoSlug
	if slug == "" {
		slug = os.Getenv("GITHUB_REPOSITORY")
	}
	if slug == "" {
		return nil, fmt.Errorf("local mode requires --repo-slug or GITHUB_REPOSITORY env var")
	}
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid repo slug %q — expected owner/name", slug)
	}
	owner, name := parts[0], parts[1]

	entry := config.RepoEntry{Repo: slug, Branch: "main"}

	var results []Result
	for taskName, task := range taskCfgs {
		if opts.TaskFilter != "" && taskName != opts.TaskFilter {
			continue
		}
		opts.Logger.Info("processing (local mode)", "repo", slug, "task", taskName)
		r := processRepo(ctx, opts, entry, owner, name, opts.LocalDir, false, task, llmClient, prHandler)
		results = append(results, r)
	}
	return results, nil
}

func runCentralMode(
	ctx context.Context, opts Options,
	reposCfg config.ReposConfig, taskCfgs map[string]config.TaskConfig,
	llmClient *llm.Client, prHandler *output.PRHandler,
) ([]Result, error) {
	var results []Result

	for _, repoEntry := range reposCfg.Repos {
		if opts.RepoFilter != "" && repoEntry.Repo != opts.RepoFilter {
			continue
		}
		parts := strings.SplitN(repoEntry.Repo, "/", 2)
		if len(parts) != 2 {
			opts.Logger.Warn("invalid repo slug, skipping", "repo", repoEntry.Repo)
			continue
		}
		owner, name := parts[0], parts[1]

		tmpDir, err := os.MkdirTemp("", "standardizer-*")
		if err != nil {
			results = append(results, Result{Repo: repoEntry.Repo, Status: "error", Detail: err.Error()})
			continue
		}

		repoURL := fmt.Sprintf("https://github.com/%s/%s.git", owner, name)
		if err := gocontext.Clone(repoURL, tmpDir); err != nil {
			os.RemoveAll(tmpDir)
			opts.Logger.Warn("clone failed", "repo", repoEntry.Repo, "error", err)
			results = append(results, Result{Repo: repoEntry.Repo, Status: "error", Detail: "clone failed: " + err.Error()})
			continue
		}

		for _, taskName := range repoEntry.Tasks {
			if opts.TaskFilter != "" && taskName != opts.TaskFilter {
				continue
			}
			task, ok := taskCfgs[taskName]
			if !ok {
				opts.Logger.Warn("task config not found", "task", taskName)
				results = append(results, Result{Repo: repoEntry.Repo, Task: taskName, Status: "error", Detail: "task config not found"})
				continue
			}
			// Apply per-repo model override.
			if repoEntry.ModelOverride != "" {
				task.Model = repoEntry.ModelOverride
			}
			opts.Logger.Info("processing", "repo", repoEntry.Repo, "task", taskName)
			r := processRepo(ctx, opts, repoEntry, owner, name, tmpDir, false, task, llmClient, prHandler)
			results = append(results, r)
		}
		os.RemoveAll(tmpDir)
	}
	return results, nil
}

// processRepo runs a single task against an already-available repo directory.
// keepDir=true means we must NOT delete repoDir (local mode).
func processRepo(
	ctx context.Context, opts Options,
	repoEntry config.RepoEntry, owner, name, repoDir string,
	_ bool, // keepDir — cleanup is handled by caller
	task config.TaskConfig,
	llmClient *llm.Client, prHandler *output.PRHandler,
) Result {
	base := Result{Repo: repoEntry.Repo, Task: task.Name}
	logger := opts.Logger.With("repo", repoEntry.Repo, "task", task.Name)

	builder := gocontext.NewBuilder(repoDir, owner, name, logger)
	data, err := builder.Build(task.Context, task.SourceGlobs, task.MaxDiffTokens)
	if err != nil {
		return Result{Repo: base.Repo, Task: base.Task, Status: "error", Detail: "context: " + err.Error()}
	}

	if agentsMD, ok := data["agents_md"]; ok && agentsMD == "" {
		logger.Info("skipping: no AGENTS.md")
		return Result{Repo: base.Repo, Task: base.Task, Status: "skipped", Detail: "no AGENTS.md"}
	}

	promptPath := filepath.Join(opts.TasksDir, task.Name+".md")
	prompt, err := tmpl.Render(promptPath, data)
	if err != nil {
		return Result{Repo: base.Repo, Task: base.Task, Status: "error", Detail: "render: " + err.Error()}
	}

	if opts.DryRun {
		logger.Info("[dry-run] prompt rendered", "len", len(prompt))
		fmt.Printf("=== [DRY RUN] %s × %s ===\n%s\n\n", repoEntry.Repo, task.Name, prompt)
		return Result{Repo: base.Repo, Task: base.Task, Status: "skipped", Detail: "[dry-run]"}
	}

	response, err := llmClient.WithModel(task.Model).Complete(
		ctx,
		"You are a precise documentation reviewer. Follow the output format exactly.",
		prompt,
	)
	if err != nil {
		return Result{Repo: base.Repo, Task: base.Task, Status: "error", Detail: "llm: " + err.Error()}
	}

	_, content, ok := output.ParseResponse(response, task.Output.SkipMarker)
	if !ok {
		logger.Info("no changes needed")
		return Result{Repo: base.Repo, Task: base.Task, Status: "no_changes"}
	}

	switch task.Output.Type {
	case "pr":
		if prHandler.GHClient == nil {
			return Result{Repo: base.Repo, Task: base.Task, Status: "error", Detail: "GH_TOKEN not set"}
		}
		prURL, err := prHandler.CreatePR(ctx, repoDir, owner, name,
			repoEntry.Branch, task.Output.BranchPrefix, task.Name, content, opts.DryRun)
		if err != nil {
			return Result{Repo: base.Repo, Task: base.Task, Status: "error", Detail: "create PR: " + err.Error()}
		}
		logger.Info("PR opened", "url", prURL)
		return Result{Repo: base.Repo, Task: base.Task, Status: "pr_opened", Detail: prURL}
	case "stdout":
		fmt.Printf("=== %s × %s ===\n%s\n\n", repoEntry.Repo, task.Name, content)
		return Result{Repo: base.Repo, Task: base.Task, Status: "no_changes"}
	default:
		return Result{Repo: base.Repo, Task: base.Task, Status: "error", Detail: "unknown output type: " + task.Output.Type}
	}
}

func loadTaskConfigs(tasksDir string) (map[string]config.TaskConfig, error) {
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return nil, fmt.Errorf("read tasks dir: %w", err)
	}
	cfgs := map[string]config.TaskConfig{}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		tc, err := config.LoadTask(filepath.Join(tasksDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("load task %s: %w", e.Name(), err)
		}
		cfgs[tc.Name] = tc
	}
	return cfgs, nil
}

func buildGHClient(ctx context.Context, token string) *github.Client {
	if token == "" {
		return nil
	}
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	return github.NewClient(oauth2.NewClient(ctx, ts))
}

var _ = time.Now
