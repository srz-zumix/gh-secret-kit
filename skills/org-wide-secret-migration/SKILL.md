---
name: org-wide-secret-migration
description: Guide for migrating all GitHub Actions secrets, variables, deploy keys, and environments across an entire organization using gh-secret-kit migrate plan, check, and runner commands.
---

# Org-Wide Secret Migration

This guide explains how to migrate all GitHub Actions secrets, variables,
deploy keys, and environments from one organization to another using
gh-secret-kit. This workflow uses `migrate plan` to generate all commands
and `migrate check` to verify the results.

## When to Use This Guide

- You are migrating an entire organization to a new organization
- You need to move all secrets, variables, environments, and deploy keys for
  many repositories at once
- You want an automated, repeatable migration process

## Prerequisites

- **Organization owner** on the source organization (required for `plan`,
  `check`, and org-level runner setup)
- **Write access** to destination repositories/organization
- If cross-host: both hosts authenticated and a destination PAT prepared

```bash
gh auth login
# For cross-host
gh auth login --hostname enterprise.internal
```

## Migration Overview

```text
1. migrate plan     → Generate all migration commands as a script
2. Review script    → Inspect and edit the generated commands
3. runner setup     → Start org-level runner (separate terminal)
4. Execute script   → Run the generated migration commands
5. migrate check    → Verify all secrets were migrated
6. runner teardown  → Clean up the runner
```

## Step 1: Scan and Plan

`migrate plan` scans the source organization for repositories with secrets,
compares them against the destination organization, and outputs executable
commands for all matching pairs.

```bash
gh secret-kit migrate plan source-org -d dest-org > migrate.sh
```

### Plan Output Contents

The generated script includes (in order):

1. **Runner setup** instruction (commented out — run manually in another terminal)
2. **Repository secret migration** commands (`migrate repo all`) for each
   repository with secrets
3. **Environment management** commands for each environment:
   - `env export | env import` pipeline (creates environment + copies variables)
   - `env variable copy` (when environment already exists at destination)
   - `migrate env all` (migrates environment secrets)
4. **Variable copy** commands (`variable copy`) for repositories with variables
5. **Deploy key migration** commands (for cross-host migrations only)
6. **Runner teardown** instruction (commented out)

Each command group is preceded by a comment listing the affected secret/variable
names.

### Plan Options

```bash
# Include --overwrite in generated commands
gh secret-kit migrate plan source-org -d dest-org --overwrite

# Include user mapping for environment reviewer conversion
gh secret-kit migrate plan source-org -d dest-org --usermap usermap.csv

# Skip deploy key scanning (saves API calls)
gh secret-kit migrate plan source-org -d dest-org --no-deploy-keys

# Custom runner label
gh secret-kit migrate plan source-org -d dest-org --runner-label custom-label

# Include --unarchive for archived repositories
gh secret-kit migrate plan source-org -d dest-org --unarchive
```

### Plan Environment Output Rules

The plan intelligently determines which commands are executable vs commented
out based on the destination state:

| Condition | `env export | import` | `env variable copy` | `migrate env all` |
| --- | --- | --- | --- |
| Destination env does not exist | executable | — | executable |
| Destination env exists, no `--overwrite`/`--usermap` | commented out | executable (if vars) | executable |
| Destination env exists, `--overwrite` set | executable | — | executable |
| Destination env exists, `--usermap` set | executable | — | executable |
| Has required reviewers, no `--usermap` | commented out | — | commented out |
| Has required reviewers, `--usermap` set | executable | — | executable |

## Step 2: Review the Script

Always review the generated script before execution:

```bash
cat migrate.sh
# or
vim migrate.sh
```

Things to check:
- Repositories that should be excluded
- Secrets that need renaming (`--rename`)
- Environments with required reviewers (may be commented out; provide `--usermap` or adjust manually)
- Commands that are commented out (plan explains why in comments)

## Step 3: Start the Runner

Run this in a **separate terminal**. It blocks until interrupted.

```bash
# Org-level runner (covers all repositories in the organization)
gh secret-kit migrate runner setup source-org
```

The org-level runner handles jobs from any repository in the organization,
so you only need one runner for the entire migration.

| Option | Default |
| --- | --- |
| `--runner-label` | gh-secret-kit-migrate |
| `--max-runners` | 2 |

## Step 4: Execute Migration

In another terminal, run the generated script:

```bash
bash migrate.sh
```

Or run commands individually for more control:

```bash
# Migrate repo secrets for a specific repo
gh secret-kit migrate repo all -s source-org/repo-a -d dest-org/repo-a

# Migrate environment secrets
gh secret-kit migrate env all \
  -s source-org/repo-a -d dest-org/repo-a \
  --src-env production --dst-env production
```

## Step 5: Verify Migration

After all commands complete, verify the migration:

```bash
gh secret-kit migrate check source-org -d dest-org
```

This checks:
- Repository secrets for all matching repositories
- Environment secrets for all matching environments
- Organization secrets

Exits with non-zero status if any secrets are missing.

### Check Individual Scopes

```bash
# Check a specific repository
gh secret-kit migrate repo check -s source-org/repo-a -d dest-org/repo-a

# Check environment secrets
gh secret-kit migrate env check \
  -s source-org/repo-a -d dest-org/repo-a \
  --src-env production --dst-env production

# Check organization secrets
gh secret-kit migrate org check -s source-org -d dest-org
```

## Step 6: Clean Up

```bash
# Stop the runner (Ctrl+C in Terminal 1, then)
gh secret-kit migrate runner teardown source-org

# Remove any leftover runners if needed
gh secret-kit migrate runner prune source-org
```

## Complete Example

```bash
# 1. Generate plan
gh secret-kit migrate plan source-org -d dest-org \
  --overwrite > migrate.sh

# 2. Review
vim migrate.sh

# 3. Start runner (Terminal 1, blocks)
gh secret-kit migrate runner setup source-org

# 4. Execute (Terminal 2)
bash migrate.sh

# 5. Verify
gh secret-kit migrate check source-org -d dest-org

# 6. Clean up (after Ctrl+C in Terminal 1)
gh secret-kit migrate runner teardown source-org
```

## Cross-Host Org-Wide Migration

When migrating to a different host, prepare the destination token first:

```bash
# Generate plan with cross-host destination
gh secret-kit migrate plan source-org \
  -d enterprise.internal/dest-org > migrate.sh

# The plan automatically includes:
# - deploy-key migrate commands (cross-host only)
# - env export | env import pipelines
```

## Listing Repositories Before Planning

Before generating a plan, you can list which repositories have secrets:

```bash
# List all repos with secrets in the org
gh secret-kit migrate list source-org

# Check a single repo
gh secret-kit migrate list -R source-org/repo-a
```

## Handling Failures

### Retry a Single Repository

If one repository fails, re-run just that command while the runner is still
active:

```bash
gh secret-kit migrate repo all -s source-org/repo-a -d dest-org/repo-a
```

### Resume After Interruption

If the runner is interrupted mid-migration:

1. Clean up any leftover runners:
   ```bash
   gh secret-kit migrate runner prune source-org
   ```

2. Restart the runner:
   ```bash
   gh secret-kit migrate runner setup source-org
   ```

3. Re-run the remaining commands from the script. Already-migrated secrets are
   skipped unless `--overwrite` is specified.

### Partial Check

Run `migrate check` to see which repositories still have missing secrets:

```bash
gh secret-kit migrate check source-org -d dest-org
```

## Organization Secrets

Organization-level secrets are migrated separately from repository secrets:

```bash
# Start runner (if not already running)
gh secret-kit migrate runner setup source-org

# Migrate org secrets
gh secret-kit migrate org all \
  -s source-org/some-repo \
  -d dest-org

# Verify
gh secret-kit migrate org check -s source-org -d dest-org
```

Note: `--src` (`-s`) for org migration takes a **repository** in the source
organization (the workflow runs in that repository's context), while `--dst`
(`-d`) takes the **destination organization name**.

## Security Notes

- Secret values are never written to disk on the runner.
- The migration workflow reads secrets via the `secrets` context and calls the
  GitHub API directly.
- Generated workflows and topic branches are cleaned up automatically by `all`
  or manually by `delete`.
