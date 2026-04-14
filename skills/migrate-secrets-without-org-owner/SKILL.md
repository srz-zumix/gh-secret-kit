---
name: migrate-secrets-without-org-owner
description: Guide for migrating GitHub Actions repository and environment secrets using gh-secret-kit with only repository admin permissions (no org owner required), using repository-level self-hosted runners.
---

# Migrate Secrets Without Org Owner Permission

This guide explains how to migrate GitHub Actions secrets when you have
**repository admin** access but **not organization owner** permissions.

By using a **repository-level runner** (`-R owner/repo`) instead of an
organization-level runner, you can migrate repository secrets and environment
secrets without org owner access.

## When to Use This Guide

- You need to migrate secrets between repositories but are not an org owner
- Your organization allows repository-level self-hosted runners
- You want to migrate repository-scoped or environment-scoped secrets

## Permissions Required

- **Repository admin** on the source repository (to register runner scale sets,
  create branches, PRs, labels, and push workflow files)
- **Write access** on the destination repository (to set secrets via API)

## Org-Level vs Repo-Level Runner

| | Org-Level Runner | Repo-Level Runner |
| --- | --- | --- |
| **Setup command** | `migrate runner setup <org>` | `migrate runner setup -R owner/repo` |
| **Teardown command** | `migrate runner teardown <org>` | `migrate runner teardown -R owner/repo` |
| **Required permission** | Organization owner | Repository admin |
| **Secret access** | Org secrets + all repo secrets | Source repo secrets + env secrets |
| **Supported scopes** | `repo`, `org`, `env` | `repo`, `env` |

The key difference is the `-R owner/repo` flag on `runner setup` / `runner teardown`,
which registers the runner at the repository level instead of the organization level.

## Migrate Repository Secrets

### Full Pipeline (Single Command)

```bash
# Terminal 1: Start a repo-level runner (blocks until interrupted)
gh secret-kit migrate runner setup -R owner/source-repo

# Terminal 2: Run full migration pipeline
gh secret-kit migrate repo all \
  -s owner/source-repo \
  -d owner/dest-repo

# After done, clean up (Terminal 1: Ctrl+C, then)
gh secret-kit migrate runner teardown -R owner/source-repo
```

### Step by Step

```bash
# Terminal 1: Start runner
gh secret-kit migrate runner setup -R owner/source-repo

# Terminal 2: Execute each step individually
gh secret-kit migrate repo init -s owner/source-repo
gh secret-kit migrate repo create -s owner/source-repo -d owner/dest-repo
gh secret-kit migrate repo run -s owner/source-repo --wait
gh secret-kit migrate repo check -s owner/source-repo -d owner/dest-repo
gh secret-kit migrate repo delete -s owner/source-repo

# Clean up runner
gh secret-kit migrate runner teardown -R owner/source-repo
```

### With Specific Secrets and Rename

```bash
# Terminal 2: Migrate only specific secrets with rename
gh secret-kit migrate repo all \
  -s owner/source-repo \
  -d owner/dest-repo \
  --secrets API_KEY,DB_PASSWORD \
  --rename API_KEY=PROD_API_KEY \
  --overwrite
```

## Migrate Environment Secrets

The same repo-level runner can access environment secrets. The generated
workflow includes the `environment` field so secrets from the source
environment are available during execution.

```bash
# Terminal 1: Start a repo-level runner
gh secret-kit migrate runner setup -R owner/source-repo

# Terminal 2: Migrate environment secrets
gh secret-kit migrate env all \
  -s owner/source-repo \
  -d owner/dest-repo \
  --src-env staging \
  --dst-env production

# Clean up
gh secret-kit migrate runner teardown -R owner/source-repo
```

### Also Copy Environment Settings and Variables

Secrets require a runner for migration, but environment settings and variables
are accessible via the API and can be copied directly:

```bash
# Copy entire environment (settings + branch policies + variables)
gh secret-kit env copy owner/dest-repo \
  -R owner/source-repo \
  --src-env staging \
  --dst-env production

# Or copy only environment variables
gh secret-kit env variable copy owner/dest-repo \
  -R owner/source-repo \
  --src-env staging \
  --dst-env production
```

## Migrate Multiple Repositories

When migrating secrets from several repositories without org owner access,
set up and tear down a runner per repository:

```bash
for repo in owner/repo-a owner/repo-b owner/repo-c; do
  # Terminal 1: Start runner for this repo
  gh secret-kit migrate runner setup -R "$repo"

  # Terminal 2: Migrate
  gh secret-kit migrate repo all -s "$repo" -d "dest-owner/${repo##*/}"

  # Teardown after each repo
  gh secret-kit migrate runner teardown -R "$repo"
done
```

## Cross-Host Migration (e.g., github.com → GHES)

```bash
# Authenticate to both hosts
gh auth login --hostname github.com
gh auth login --hostname enterprise.internal

# Terminal 1: Repo-level runner on source
gh secret-kit migrate runner setup -R owner/source-repo

# Terminal 2: Migrate repo secrets to GHES
gh secret-kit migrate repo all \
  -s owner/source-repo \
  -d enterprise.internal/owner/dest-repo

# Migrate environment secrets
gh secret-kit migrate env all \
  -s owner/source-repo \
  -d enterprise.internal/owner/dest-repo \
  --src-env production \
  --dst-env production

# Copy variables (no runner needed)
gh secret-kit variable copy enterprise.internal/owner/dest-repo \
  -R owner/source-repo --overwrite

# Migrate deploy keys (cross-host only)
gh secret-kit deploy-key migrate enterprise.internal/owner/dest-repo \
  -R owner/source-repo

# Clean up
gh secret-kit migrate runner teardown -R owner/source-repo
```

## Available Commands Without Org Owner

| Command | Available? | Notes |
| --- | --- | --- |
| `migrate runner setup -R owner/repo` | Yes | Repo-level runner |
| `migrate runner teardown -R owner/repo` | Yes | Repo-level runner |
| `migrate runner prune -R owner/repo` | Yes | Repo-level runner |
| `migrate repo all/init/create/run/check/delete` | Yes | Repo secrets |
| `migrate env all/init/create/run/check/delete` | Yes | Env secrets |
| `migrate list -R owner/repo` | Yes | Single repo check |
| `variable copy` | Yes | API-accessible |
| `env copy/export/import` | Yes | API-accessible |
| `deploy-key add/delete/get/list/migrate` | Yes | API-accessible |
| `migrate org all/init/create/run/check/delete` | **No** | Requires org owner |
| `migrate runner setup <org>` | **No** | Requires org owner |
| `migrate plan <org>` | **No** | Requires org owner |
| `migrate check <org>` | **No** | Requires org owner |
| `migrate list <org>` | **No** | Requires org owner |
| `deploy-key setting` | **No** | Requires org owner |

## Limitations

- **Organization secrets cannot be migrated** with a repo-level runner. Org
  secrets require an org-level runner (`migrate runner setup <org>`), which
  needs org owner permissions.
- **`migrate plan` and `migrate check <org>` are not usable** without org
  owner permissions, as they scan all repositories in the organization.
- Each repo requires its own runner setup/teardown cycle when migrating
  multiple repositories.
