# ffreis-workflow-ai-standardizer

<!-- ffreis-badges:start -->
[![CI](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/FelipeFuhr/ffreis-badges/main/badges/ffreis-workflow-ai-standardizer/ci.json)](https://github.com/FelipeFuhr/ffreis-workflow-ai-standardizer/actions) [![Latest version](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/FelipeFuhr/ffreis-badges/main/badges/ffreis-workflow-ai-standardizer/version.json)](https://github.com/FelipeFuhr/ffreis-workflow-ai-standardizer/releases) [![License](https://img.shields.io/endpoint?url=https://raw.githubusercontent.com/FelipeFuhr/ffreis-badges/main/badges/ffreis-workflow-ai-standardizer/license.json)](https://github.com/FelipeFuhr/ffreis-workflow-ai-standardizer/blob/main/LICENSE)
<!-- ffreis-badges:end -->

A Go CLI (`standardizer`) that runs AI-assisted maintenance tasks across a fleet of repositories. For each configured repo it gathers context (git diffs, file reads, directory trees), renders a per-task prompt, calls any OpenAI-compatible LLM, parses a marker-based response, and either opens a pull request or prints the result. The single bundled task, `detect-drift`, reviews a repo's `AGENTS.md` against the source changes made since that file was last updated and opens a PR proposing an update when the documentation has drifted. Tasks are pure configuration plus a prompt template — adding one requires no Go code changes.

## What it does

The tool iterates over (repo × task) pairs. For each pair it:

1. **Gathers context.** A context builder resolves the keys listed in the task config against the repo working tree. Available keys: `agents_md` (the repo's `AGENTS.md`), `diff_since_agents_update` (git diff from the last commit touching `AGENTS.md` to `HEAD`, filtered to `source_globs` and truncated to `max_diff_tokens`), `changed_files_list`, `readme`, and `directory_tree`. If a task requests `agents_md` and the file is absent, the pair is skipped.
2. **Renders the prompt.** The task's `tasks/<name>.md` file is a Go `text/template` rendered with the gathered context (`{{index . "key"}}`).
3. **Calls the LLM.** A single-turn chat completion via an OpenAI-compatible client (`go-openai`), with 3 retries (linear backoff) and a 120s timeout. The provider is chosen by `LLM_BASE_URL`; any compatible endpoint works (Anthropic via proxy, OpenAI, GitHub Models, Ollama, etc.). Default model is `claude-sonnet-4-6`.
4. **Parses the response.** The model is instructed to reply either with the task's skip marker (e.g. `NO_CHANGES_NEEDED`) or with `<action>...</action><content>...</content>`. The parser ignores any preamble; a skip marker or a missing action block yields a `no_changes` result.
5. **Emits output.** Per the task's `output.type`:
   - `pr` — creates a branch (`output.branch_prefix` + task name) off the repo's configured base branch, commits the new content, and opens a PR (requires `GH_TOKEN`).
   - `stdout` — prints the content; recorded as `no_changes`.

Per-pair results (`skipped` / `pr_opened` / `no_changes` / `error`) are printed as a summary table, optionally written as `summary.json` to `--output-dir`, and (under GitHub Actions) appended to `$GITHUB_STEP_SUMMARY`. A non-zero exit is returned if any task errored (except in dry-run).

### Two run modes

- **Central mode** (default): reads `config/repos.yaml`, clones each listed repo into a temp dir, runs its assigned tasks, then removes the temp dir. Each repo entry sets `repo` (owner/name), `tasks`, an optional `branch` (default `main`), and an optional `model_override`. Used by the scheduled `run.yml` workflow.
- **Local mode** (`--local-dir <path> --repo-slug owner/name`): operates on a pre-cloned directory with no clone or cleanup — the caller owns the directory lifecycle. Used when a repo invokes the reusable workflow from `devops/ffreis-workflows-ai` (`ai-standardize.yml`) on itself. `--repo-slug` falls back to `GITHUB_REPOSITORY`.

### Task definition

A task is two files in `tasks/`:

- `<name>.yaml` — `name`, `description`, `model`, `context` (required, non-empty list of context keys), `source_globs`, `max_diff_tokens` (default 3000), and `output` (`type` default `stdout`, plus `branch_prefix` and `skip_marker` for `pr` output).
- `<name>.md` — the Go `text/template` prompt.

The runner discovers tasks by scanning `tasks/*.yaml`. The bundled `detect-drift` task uses `pr` output with branch prefix `context-keeper/drift-` and skip marker `NO_CHANGES_NEEDED`.

## Usage

```bash
make build   # builds ./bin/standardizer

# List and validate task configs
./bin/standardizer tasks list
./bin/standardizer tasks validate

# Dry run (renders + prints prompts; no API calls, git writes, or PRs)
./bin/standardizer run --repo FelipeFuhr/ffreis-siteops --dry-run

# Local mode (as called from ffreis-workflows-ai)
./bin/standardizer run --local-dir ../repo --repo-slug FelipeFuhr/ffreis-siteops

# Full central run
LLM_API_KEY=... GH_TOKEN=... ./bin/standardizer run
```

### `run` flags

| Flag | Default | Purpose |
|---|---|---|
| `--repos-config` | `config/repos.yaml` | repos config path (central mode) |
| `--tasks-dir` | `tasks` | tasks directory |
| `--output-dir` | (none) | write `summary.json` here |
| `--task` | (all) | run only this task |
| `--repo` | (all) | filter central mode to one `owner/name` |
| `--model` | `claude-sonnet-4-6` | model override |
| `--base-url` | (env) | LLM base URL (any OpenAI-compatible endpoint) |
| `--dry-run` | `false` | print rendered prompts; skip API calls and git writes |
| `--local-dir` | (none) | use a pre-cloned directory instead of cloning (local mode) |
| `--repo-slug` | (none) | `owner/name` of the local repo (required with `--local-dir`) |

### Environment

- `LLM_API_KEY` (or `ANTHROPIC_API_KEY` / `OPENAI_API_KEY`, first non-empty) — required unless `--dry-run`.
- `LLM_BASE_URL` — provider endpoint (overridden by `--base-url`).
- `LLM_MODEL` — model (overridden by `--model`; defaults to `claude-sonnet-4-6`).
- `GH_TOKEN` — required for `pr` output; without it, `pr` tasks error.
- `GITHUB_REPOSITORY` — fallback for `--repo-slug` in local mode.
- `GITHUB_STEP_SUMMARY` — when set (CI), the results table is appended to it.

### As a scheduled workflow

`.github/workflows/run.yml` builds the binary and runs it monthly (and on `workflow_dispatch` with `task` / `repo` / `model` / `dry_run` inputs), wiring `LLM_API_KEY`, `LLM_BASE_URL`, and `GH_TOKEN` (from `FLEET_WRITE_TOKEN`) and uploading `summary.json` as an artifact.

## Development

Go module (`go 1.25.x`); Cobra CLI. Layout:

```
cmd/standardizer/   Cobra CLI (run; tasks list/validate)
internal/config/    load repos.yaml and task YAML
internal/context/   context providers (git clone, diff, file reads, tree)
internal/llm/        go-openai wrapper (retries, token logging)
internal/output/     response parser, PR creator, summary writer
internal/runner/     central and local run modes
internal/tmpl/       text/template prompt rendering
tasks/               *.yaml task configs + *.md prompt templates
config/repos.yaml    repos monitored in central mode
```

Common Make targets: `build`, `test` (`go test -race -shuffle=on ./...`), `lint` (`go vet` + golangci-lint), `fmt` / `fmt-check`, `vet`, `security` (govulncheck), `secrets-scan-staged` (gitleaks), `clean`. `make hooks` / `make setup` bootstrap lefthook hooks from `ffreis-platform-standards`; `make ci-local` runs the GitHub Actions workflows locally via `act`.

Adding a task: create `tasks/<name>.yaml` and `tasks/<name>.md`, assign the task to the relevant repos in `config/repos.yaml`, then run `./bin/standardizer tasks validate`.

Note: this is a public repository. Never name private repos in commit messages, PR titles, or PR bodies — use generic terms ("the fleet inventory", "a private consumer", "internal infra").

## License

Proprietary — All Rights Reserved. Copyright (c) 2026 Felipe Fuhr. No part of this software may be copied, modified, distributed, sublicensed, or used in any form without prior written permission of the copyright holder. Licensing inquiries: felipefuhr7@gmail.com. See [LICENSE](LICENSE).
