# Migrating GitHub Actions Secrets

This guide explains how to migrate GitHub Actions secrets between repositories, organizations, and environments using `gh secret-kit migrate`.

## How It Works

Since the GitHub API does not expose secret values, `gh secret-kit migrate` uses a self-hosted runner to read secrets at runtime and write them to the destination via API.

The overall flow is:

```text
[Terminal 1]  migrate runner setup   → Register ephemeral runner on source
[Terminal 2]  migrate {scope} init   → Push stub workflow, open draft PR
              migrate {scope} create → Generate and push migration workflow
              migrate {scope} run    → Trigger workflow via label
              migrate {scope} check  → Verify secrets were migrated
              migrate {scope} delete → Clean up branch and PR
[Terminal 1]  migrate runner teardown → Stop and unregister runner
```

The `scope` is one of `repo`, `org`, or `env`.

> **Note**: Dependabot secrets are **not** supported. Dependabot secrets can only be accessed by workflows triggered by Dependabot.

## Prerequisites

- `gh` CLI authenticated with sufficient permissions on the source repository
- The scaleset runner environment is assumed to have `gh` authenticated with sufficient permissions to the destination as well — this covers most same-host migrations without any extra options
- The source repository must be able to run GitHub Actions workflows

## Quick Start: Use `all`

Each scope provides an `all` subcommand that runs all steps (init → create → run → check → delete) in a single call. You still need to start the runner listener first.

### Migrate repository secrets (quick)

```sh
# Terminal 1: Start runner listener (blocks until interrupted)
gh secret-kit migrate runner setup -R owner/source-repo

# Terminal 2: Run all steps at once
gh secret-kit migrate repo all \
  -s owner/source-repo \
  -d owner/dest-repo

# After Terminal 2 finishes, interrupt Terminal 1 (Ctrl+C) then clean up
gh secret-kit migrate runner teardown -R owner/source-repo
```

### Migrate organization secrets (quick)

```sh
# Terminal 1
gh secret-kit migrate runner setup -R org/some-repo

# Terminal 2
gh secret-kit migrate org all \
  -s org/some-repo \
  -d dest-org

# Clean up
gh secret-kit migrate runner teardown -R org/some-repo
```

### Migrate environment secrets (quick)

```sh
# Terminal 1
gh secret-kit migrate runner setup -R owner/repo

# Terminal 2
gh secret-kit migrate env all \
  -s owner/repo \
  -d owner/dest-repo \
  --src-env staging \
  --dst-env production

# Clean up
gh secret-kit migrate runner teardown -R owner/repo
```

## Step-by-Step Guide

Use the individual subcommands when you need more control over each step (e.g., to review the generated workflow, run multiple scopes against the same runner, or retry a single step).

### Step 1: Start the Runner

Run this in **Terminal 1**. It blocks until interrupted (Ctrl+C).

```sh
# For a repository-scoped runner
gh secret-kit migrate runner setup -R owner/source-repo

# For an organization-scoped runner
gh secret-kit migrate runner setup owner-org
```

| Option | Description |
| --- | --- |
| `--repo` / `-R` | Source repository (`owner/repo`). Omit to use org scope. |
| `--runner-label` | Custom runner label (default: `gh-secret-kit-migrate`) |
| `--max-runners` | Maximum concurrent runners (default: `2`) |

### Step 2: Init

Creates a topic branch, pushes a stub workflow, and opens a draft PR on the source repository. This makes GitHub recognise the workflow file.

```sh
gh secret-kit migrate repo init -s owner/source-repo
# or
gh secret-kit migrate org init -s org/some-repo
# or
gh secret-kit migrate env init -s owner/source-repo
```

| Option | Description |
| --- | --- |
| `--src` / `-s` | Source repository (default: current repository) |
| `--branch` | Topic branch name (default: `gh-secret-kit-migrate`) |
| `--label` | Trigger label name (default: `gh-secret-kit-migrate`) |
| `--workflow-name` | Workflow file name (default: `gh-secret-kit-migrate`) |
| `--unarchive` | Unarchive repository temporarily if archived |

### Step 3: Create

Generates the migration workflow YAML and pushes it to the topic branch.

```sh
# Repository secrets
gh secret-kit migrate repo create \
  -s owner/source-repo \
  -d owner/dest-repo

# Organization secrets
gh secret-kit migrate org create \
  -s org/some-repo \
  -d dest-org

# Environment secrets
gh secret-kit migrate env create \
  -s owner/source-repo \
  -d owner/dest-repo \
  --src-env staging \
  --dst-env production
```

| Option | Description |
| --- | --- |
| `--src` / `-s` | Source repository (default: current repository) |
| `--dst` / `-d` | Destination repository or organization (required) |
| `--secrets` | Secret names to migrate (comma-separated; default: all) |
| `--rename` | Rename mapping `OLD=NEW` (repeatable) |
| `--overwrite` | Overwrite existing secrets at destination |
| `--runner-label` | Runner label in workflow `runs-on` (default: `gh-secret-kit-migrate`) |
| `--src-env` | Source environment name (env scope only, required) |
| `--dst-env` | Destination environment name (env scope only, required) |

### Step 4: Run

Triggers the migration workflow by toggling the trigger label on the draft PR.

```sh
gh secret-kit migrate repo run -s owner/source-repo
# or
gh secret-kit migrate org run -s org/some-repo
# or
gh secret-kit migrate env run -s owner/source-repo
```

| Option | Description |
| --- | --- |
| `--src` / `-s` | Source repository (default: current repository) |
| `--wait` / `-w` | Wait for workflow completion |
| `--timeout` | Timeout when waiting (e.g., `5m`, `1h`; default: `10m`) |

### Step 5: Check

Compares secrets between source and destination. Exits with non-zero status if any secrets are missing.

```sh
# Repository secrets
gh secret-kit migrate repo check \
  -s owner/source-repo \
  -d owner/dest-repo

# Organization secrets
gh secret-kit migrate org check \
  -s source-org \
  -d dest-org

# Environment secrets
gh secret-kit migrate env check \
  -s owner/source-repo \
  -d owner/dest-repo \
  --src-env staging \
  --dst-env production
```

| Option | Description |
| --- | --- |
| `--src` / `-s` | Source repository or organization |
| `--dst` / `-d` | Destination repository or organization |
| `--secrets` | Secret names to check (default: all) |
| `--rename` | Rename mappings to apply when comparing |

### Step 6: Delete

Closes the draft PR and deletes the topic branch.

```sh
gh secret-kit migrate repo delete -s owner/source-repo
# or
gh secret-kit migrate org delete -s org/some-repo
# or
gh secret-kit migrate env delete -s owner/source-repo
```

### Step 7: Teardown Runner

Stop and unregister the runner. Run this after interrupting the runner listener.

```sh
gh secret-kit migrate runner teardown -R owner/source-repo
# or for org scope
gh secret-kit migrate runner teardown owner-org
```

### Cleaning Up Leftover Runners

If a previous setup was interrupted without teardown, orphaned runners may remain registered in GitHub. Use `runner prune` to remove them:

```sh
# Preview (no deletion)
gh secret-kit migrate runner prune --dry-run owner-org

# Remove all gh-secret-kit- runners with the default label
gh secret-kit migrate runner prune owner-org

# Remove all gh-secret-kit- runners regardless of label
# NOTE: The empty string argument must be passed explicitly; keep the quotes.
gh secret-kit migrate runner prune --runner-label "" owner-org
# or equivalently (no quotes required in most shells)
gh secret-kit migrate runner prune --runner-label= owner-org
```

| Option | Description |
| --- | --- |
| `--repo` / `-R` | Source repository (`owner/repo`). Omit to use org scope. |
| `--runner-label` | Only remove runners with this label (default: `gh-secret-kit-migrate`; pass an empty value as `--runner-label ""` (quotes required) or `--runner-label=` to target all gh-secret-kit runners) |
| `--dry-run` / `-n` | Preview without deleting |

## Checking Migration Status Across an Organization

Use `migrate check` to scan an entire organization and verify all secrets have been migrated:

```sh
gh secret-kit migrate check source-org -d dest-org
```

This checks repository secrets, environment secrets, and organization secrets for all matching repository pairs and prints a pass/fail summary.

## Planning a Migration

Use `migrate plan` to preview the migration commands without executing them:

```sh
gh secret-kit migrate plan source-org -d dest-org
```

This outputs the `migrate repo all` commands for all repositories with secrets that exist in both organizations.

## Listing Repositories with Secrets

```sh
# Scan an organization
gh secret-kit migrate list source-org

# Check a single repository
gh secret-kit migrate list -R owner/repo
```

## Common Scenarios

### Migrate specific secrets with renaming

```sh
# Terminal 1
gh secret-kit migrate runner setup -R owner/source-repo

# Terminal 2
gh secret-kit migrate repo all \
  -s owner/source-repo \
  -d owner/dest-repo \
  --secrets API_KEY,DB_PASSWORD \
  --rename API_KEY=PROD_API_KEY \
  --overwrite

# Clean up
gh secret-kit migrate runner teardown -R owner/source-repo
```

### Cross-host migration (GitHub.com → GHES)

```sh
# Terminal 1
gh secret-kit migrate runner setup -R owner/source-repo

# Terminal 2
gh secret-kit migrate repo all \
  -s owner/source-repo \
  -d ghes.example.com/owner/dest-repo \
  --dst-token DST_PAT

# Clean up
gh secret-kit migrate runner teardown -R owner/source-repo
```

### Migrate multiple repositories using plan output

```sh
# Preview commands
gh secret-kit migrate plan source-org -d dest-org

# Start runner once for the whole org
gh secret-kit migrate runner setup source-org

# Run each generated command from plan output
gh secret-kit migrate repo all -s source-org/repo-a -d dest-org/repo-a
gh secret-kit migrate repo all -s source-org/repo-b -d dest-org/repo-b
# ...

gh secret-kit migrate runner teardown source-org
```

## Security Notes

- Secret values are **never written to disk** on the runner.
- The migration workflow reads secrets via the `secrets` context and calls the GitHub API directly.
- The generated workflow and topic branch are cleaned up by `delete`.
- `--dst-token` is **rarely needed**. It specifies the name of a secret variable (e.g., `DST_PAT`) registered on the source repository, whose value is used as a PAT for the destination. The token value is never embedded in the workflow YAML; it is read at runtime via `${{ secrets.DST_PAT }}`. Use this only when the scaleset runner does not have `gh` authenticated for the destination host (e.g., cross-host migration to GHES).
