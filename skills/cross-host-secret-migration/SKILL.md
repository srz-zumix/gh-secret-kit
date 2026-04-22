---
name: cross-host-secret-migration
description: Guide for migrating GitHub Actions secrets, variables, deploy keys, and environments across different GitHub hosts (e.g., github.com to GitHub Enterprise Server) using gh-secret-kit.
---

# Cross-Host Migration (e.g., github.com → GHES)

This guide explains how to migrate GitHub Actions secrets, variables, deploy
keys, and environments from one GitHub host to another (e.g., github.com to
GitHub Enterprise Server, or between two GHES instances) using gh-secret-kit.

## When to Use This Guide

- You are moving repositories from github.com to GitHub Enterprise Server (or vice versa)
- You are migrating between two different GHES instances
- You need to transfer secrets, variables, deploy keys, and environment
  configurations across hosts

## Prerequisites

### Authentication

You must authenticate to **both** hosts before starting:

```bash
gh auth login --hostname github.com
gh auth login --hostname enterprise.internal
```

For GHES-to-GHES migration, authenticate to both GHES hosts:

```bash
gh auth login --hostname ghes1.internal
gh auth login --hostname ghes2.internal
```

### Argument Format

All flags that accept a repository (`-R`, `-s`) and destination arguments (`-d`) support the `[HOST/]OWNER/REPO` format. For cross-host operations, always include the host prefix:

```
# Source repository on a non-default host
-R ghes1.internal/owner/source-repo
-s ghes1.internal/owner/source-repo

# Destination repository or organization
-d ghes2.internal/owner/dest-repo      # repo destination
-d ghes2.internal/dest-org             # org destination (HOST/ORG)
```

## What Can Be Migrated Cross-Host

| Resource | Method | Runner Required? |
| --- | --- | --- |
| Repository secrets | `migrate repo all` | Yes |
| Environment secrets | `migrate env all` | Yes |
| Organization secrets | `migrate org all` | Yes |
| Repository variables | `variable copy` | No (API-accessible) |
| Environment variables | `env variable copy` or `env copy` | No (API-accessible) |
| Environment settings | `env copy` or `env export/import` | No (API-accessible) |
| Deploy keys | `deploy-key migrate` | No (API-accessible) |

## Step-by-Step: Full Cross-Host Migration

### 1. Migrate Deploy Keys

GitHub does not allow the same public key on multiple repositories on the same
host, but cross-host migration is supported.

```bash
gh secret-kit deploy-key migrate enterprise.internal/owner/dest-repo \
  -R owner/source-repo

# Exclude specific keys by title (substring match)
gh secret-kit deploy-key migrate enterprise.internal/owner/dest-repo \
  -R owner/source-repo --exclude test,temporary
```

### 2. Copy Variables (No Runner Needed)

Variables are API-accessible and can be copied directly:

```bash
# Copy repository variables
gh secret-kit variable copy enterprise.internal/owner/dest-repo \
  -R owner/source-repo

# Copy with overwrite
gh secret-kit variable copy enterprise.internal/owner/dest-repo \
  -R owner/source-repo --overwrite
```

### 3. Copy Environments (No Runner Needed)

Environment settings, deployment branch policies, and **variables** can be copied
or exported/imported without a runner. **Secrets are not included** — use
`migrate env all` (Step 6) for environment secrets.

`--dst-env` defaults to the same name as `--src-env` when omitted.

```bash
# Copy entire environment (settings + branch policies + variables)
# --dst-env defaults to same name as --src-env when omitted
gh secret-kit env copy enterprise.internal/owner/dest-repo \
  -R owner/source-repo \
  --src-env production

# Copy to a different destination environment name
gh secret-kit env copy enterprise.internal/owner/dest-repo \
  -R owner/source-repo \
  --src-env staging --dst-env production

# Or export and import for more control
gh secret-kit env export -R owner/source-repo -o envs.yaml
gh secret-kit env import envs.yaml -R enterprise.internal/owner/dest-repo

# With user mapping for reviewer login conversion
gh secret-kit env import envs.yaml \
  -R enterprise.internal/owner/dest-repo \
  --usermap usermap.csv
```

### 4. Start the Runner

The runner must be started on the **source** host (i.e., the host you are migrating *from*, regardless of whether it is github.com or a GHES instance).

```bash
# Repo-level runner (repo admin required)
gh secret-kit migrate runner setup -R owner/source-repo

# Or org-level runner (org owner required)
gh secret-kit migrate runner setup source-org

# Place runner in a specific runner group (created if not found)
gh secret-kit migrate runner setup source-org --runner-group migration-runners
```

### 5. Migrate Repository Secrets

```bash
gh secret-kit migrate repo all \
  -s owner/source-repo \
  -d enterprise.internal/owner/dest-repo
```

### 6. Migrate Environment Secrets

```bash
gh secret-kit migrate env all \
  -s owner/source-repo \
  -d enterprise.internal/owner/dest-repo \
  --src-env production \
  --dst-env production
```

### 7. Migrate Organization Secrets (If Org Owner)

```bash
gh secret-kit migrate org all \
  -s source-org/some-repo \
  -d enterprise.internal/dest-org
```

### 8. Clean Up

```bash
# Stop and unregister the runner
gh secret-kit migrate runner teardown -R owner/source-repo
```

## Org-Wide Cross-Host Migration Using Plan

For organizations with many repositories, use `migrate plan` to generate all
migration commands at once:

```bash
# Generate migration plan (org owner required)
gh secret-kit migrate plan source-org \
  -d enterprise.internal/dest-org > migrate.sh

# Review the generated script
cat migrate.sh

# Start the runner (optionally in a runner group)
gh secret-kit migrate runner setup source-org
# Or: gh secret-kit migrate runner setup source-org --runner-group migration-runners

# Execute migration (from another terminal)
bash migrate.sh

# Verify
gh secret-kit migrate check source-org \
  -d enterprise.internal/dest-org

# Tear down
gh secret-kit migrate runner teardown source-org
```

The plan also generates `deploy-key migrate` commands when the source and
destination are on different hosts.

## Cross-Host Without Org Owner

If you do not have org owner access, use a repo-level runner and migrate
per-repository:

```bash
# Start repo-level runner
gh secret-kit migrate runner setup -R owner/source-repo

# Migrate everything for this repo
gh secret-kit deploy-key migrate enterprise.internal/owner/dest-repo \
  -R owner/source-repo --exclude test

gh secret-kit variable copy enterprise.internal/owner/dest-repo \
  -R owner/source-repo --overwrite

gh secret-kit env copy enterprise.internal/owner/dest-repo \
  -R owner/source-repo --src-env production

gh secret-kit migrate repo all \
  -s owner/source-repo \
  -d enterprise.internal/owner/dest-repo

gh secret-kit migrate env all \
  -s owner/source-repo \
  -d enterprise.internal/owner/dest-repo \
  --src-env production --dst-env production

# Clean up
gh secret-kit migrate runner teardown -R owner/source-repo
```

## Security Notes

- Secret values are never written to disk on the runner.
- The generated workflow and topic branch are cleaned up by `delete` (or
  automatically when using `all`).

## Destination Argument Format

Cross-host destinations use the `HOST/OWNER/REPO` or `HOST/ORG` format:

```bash
# Repository on GHES
enterprise.internal/owner/repo

# Organization on GHES
enterprise.internal/org-name
```

When using `variable copy` or `env copy`, destination arguments without a host
prefix default to the source host. Use `--dst-host` to override:

```bash
gh secret-kit variable copy owner/dest-repo \
  --dst-host enterprise.internal \
  -R owner/source-repo
```
