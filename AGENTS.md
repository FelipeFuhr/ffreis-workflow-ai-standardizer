# Agent Context

**This repo:** `ffreis-workflow-ai-standardizer` — Go CLI that runs AI-assisted
maintenance tasks across repos. Iterates repos × tasks, gathers context (git diff,
file reads), calls any OpenAI-compatible LLM, and opens PRs or issues.

Exposed as a reusable GitHub Actions workflow via `devops/ffreis-workflows-ai`
(`ai-standardize.yml`). Can also be run centrally (scheduled, monitoring many repos)
or locally inside a repo's own CI pipeline.

## Two modes

- **Central mode** (default): reads `config/repos.yaml`, clones each repo into a temp
  dir, iterates repos × tasks. Used in the scheduled `run.yml` workflow.
- **Local mode** (`--local-dir <path> --repo-slug owner/name`): uses a pre-cloned
  directory; no clone step. Used by `ffreis-workflows-ai/ai-standardize.yml` when
  a repo calls the reusable workflow on itself.

## Non-obvious facts

- **Model-agnostic via OpenAI-compatible client.** `LLM_BASE_URL` + `LLM_API_KEY`
  determine the provider. Works with Anthropic's endpoint, LiteLLM proxy, GitHub
  Models, Ollama, etc.

- **Tasks are pure config + prompt — no Go code changes needed.** Add
  `tasks/<name>.yaml` + `tasks/<name>.md`. The runner discovers task files by
  scanning `tasks/*.yaml`.

- **Response protocol is marker-based.** Prompts instruct the model to respond with
  `<action>...</action><content>...</content>` or `NO_CHANGES_NEEDED`. The parser
  ignores any preamble. This is intentional: it survives model switches.

- **`diff_since_agents_update`** diffs from the last commit that touched `AGENTS.md`
  to `HEAD`, filtered to `source_globs`. Returns a descriptive string (not an error)
  if AGENTS.md has no git history.

- **Local mode does NOT clone or clean up the directory.** The caller owns the dir
  lifecycle. Do not add cleanup logic to local mode.

- **`--dry-run` prints the rendered prompt and skips LLM calls, git writes, and PR
  creation.** Use it to validate context gathering and prompt rendering without cost.

## Structure

```
cmd/standardizer/       ← Cobra CLI (run, tasks list/validate)
internal/config/        ← load repos.yaml and task YAML
internal/context/       ← context providers (git clone, diff, file reads)
internal/llm/           ← go-openai wrapper (retries, token logging)
internal/output/        ← response parser, PR creator, artifact writer
internal/runner/        ← central and local run modes
internal/tmpl/          ← Go text/template prompt rendering
tasks/                  ← *.yaml task configs + *.md prompt templates
config/repos.yaml       ← repos monitored in central mode
```

## Build/run

```bash
make build

# Validate task configs
./bin/standardizer tasks validate

# Dry run against one repo (central mode, no clone needed)
./bin/standardizer run --repo FelipeFuhr/ffreis-siteops --dry-run

# Local mode (as called from ffreis-workflows-ai)
./bin/standardizer run --local-dir ../repo --repo-slug FelipeFuhr/ffreis-siteops

# Full central run
LLM_API_KEY=sk-... GH_TOKEN=ghp_... ./bin/standardizer run
```

## Adding a new task

1. Create `tasks/<name>.yaml` — context keys, model, output config.
2. Create `tasks/<name>.md` — prompt template (Go `text/template`; `{{index . "key"}}`).
3. Add task name to `config/repos.yaml` for repos you want it to run on.
4. Run `./bin/standardizer tasks validate` to verify.

## Public repo — private-repo hygiene

This is a **public** GitHub repository. When writing commit messages, PR titles,
PR descriptions, or any other user-visible text, **never name private repos** —
website content, inventory, infra, Lambda, or data repos that are not publicly
listed. Use generic terms instead: "the fleet inventory", "a private consumer",
"internal infra", "private data repo", etc.

## Keeping this file current

- **If you discover a fact not reflected here:** add it before finishing your task.
- **If something here is wrong or outdated:** correct it in the same commit.
- **If you rename a file, command, or concept:** update the reference here.
