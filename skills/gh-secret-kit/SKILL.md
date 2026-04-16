---
name: gh-secret-kit
description: GitHub CLI extension (gh secret-kit) for managing GitHub Actions secrets, variables, deploy keys, and environments — including cross-host migration of secrets using self-hosted runners.
---

# gh-secret-kit

Comprehensive reference for gh-secret-kit — a GitHub CLI extension for
secret-related operations: managing deploy keys, environment configurations,
variables, and migrating GitHub Actions secrets between repositories,
organizations, and environments.

Version: 0.8.0

## Prerequisites

### Installation

```bash
gh extension install srz-zumix/gh-secret-kit
```

### Authentication

```bash
# Login to GitHub CLI
gh auth login

# For cross-host migration, authenticate to both hosts
gh auth login --hostname github.com
gh auth login --hostname enterprise.internal
```

## CLI Structure

```
gh secret-kit                           # Root command
├── variable                            # GitHub Actions variables
│   └── copy                            # Copy variables to destinations
├── deploy-key                          # Repository deploy keys
│   ├── add                             # Add a deploy key
│   ├── delete (rm)                     # Delete a deploy key
│   ├── get (view)                      # Show deploy key details
│   ├── list (ls)                       # List deploy keys
│   ├── migrate                         # Migrate deploy keys across hosts
│   └── setting                         # Org deploy key enablement
├── env                                 # Environment resources
│   ├── copy                            # Copy environment to destinations
│   ├── export                          # Export environments to YAML/JSON
│   ├── get (view)                      # Show environment details
│   ├── import                          # Import environment configurations
│   ├── list (ls)                       # List environments
│   └── variable                        # Environment variables
│       └── copy                        # Copy env variables to destinations
├── migrate                             # Secret migration
│   ├── check                           # Verify org-wide migration status
│   ├── list                            # List repos with secrets
│   ├── plan                            # Generate migration commands
│   ├── env                             # Environment secrets
│   │   ├── all                         # Full pipeline
│   │   ├── check                       # Verify env secrets
│   │   ├── create                      # Generate migration workflow
│   │   ├── delete                      # Clean up PR and branch
│   │   ├── init                        # Initialize stub workflow via draft PR
│   │   └── run                         # Trigger migration workflow
│   ├── org                             # Organization secrets
│   │   ├── all                         # Full pipeline
│   │   ├── check                       # Verify org secrets
│   │   ├── create                      # Generate migration workflow
│   │   ├── delete                      # Clean up PR and branch
│   │   ├── init                        # Initialize stub workflow via draft PR
│   │   └── run                         # Trigger migration workflow
│   ├── repo                            # Repository secrets
│   │   ├── all                         # Full pipeline
│   │   ├── check                       # Verify repo secrets
│   │   ├── create                      # Generate migration workflow
│   │   ├── delete                      # Clean up PR and branch
│   │   ├── init                        # Initialize stub workflow via draft PR
│   │   └── run                         # Trigger migration workflow
│   └── runner                          # Self-hosted runner management
│       ├── setup                       # Register and start runner
│       ├── teardown                    # Unregister and stop runner
│       └── prune                       # Remove leftover runners
└── completion                          # Shell completion
```

## Global Flags

| Flag | Description |
| --- | --- |
| `--help` / `-h` | Show help for command |
| `--log-level` / `-L` | Set log level |
| `--read-only` | Run in read-only mode (prevent write operations) |
| `--version` | Show version |

## Variables (gh secret-kit variable)

### Copy Variables

```bash
# Copy all variables from current repo to destination
gh secret-kit variable copy owner/dest-repo

# Copy all variables from current repo to multiple destinations
gh secret-kit variable copy owner/repo1 owner/repo2

# Copy specific variables only
gh secret-kit variable copy owner/dest-repo --variables VAR1,VAR2

# Copy organization variables
gh secret-kit variable copy dest-org --owner source-org

# Overwrite existing variables
gh secret-kit variable copy owner/dest-repo --overwrite

# Copy with destination host
gh secret-kit variable copy owner/dest-repo --dst-host enterprise.internal
```

| Flag | Description | Default |
| --- | --- | --- |
| `--dst-host string` | Host to apply to destinations without one | source host |
| `--owner string` | Source organization (mutually exclusive with `--repo`) | |
| `--overwrite` | Overwrite existing variables | false |
| `--repo string` / `-R` | Source repository | current repo |
| `--variables strings` | Specific variable names (comma-separated or repeated) | all |

## Deploy Keys (gh secret-kit deploy-key)

### Add Deploy Key

```bash
# Add deploy key from string
gh secret-kit deploy-key add "ssh-rsa AAAA..." --title "My Key"

# Add deploy key from file
gh secret-kit deploy-key add --key-file ~/.ssh/id_rsa.pub --title "CI Key"

# Add read-only deploy key
gh secret-kit deploy-key add --key-file ~/.ssh/id_rsa.pub --title "CI Key" --read-only

# Add to specific repository
gh secret-kit deploy-key add --key-file ~/.ssh/id_rsa.pub --title "CI Key" -R owner/repo
```

| Flag | Description | Default |
| --- | --- | --- |
| `--key-file string` / `-f` | Path to public key file | |
| `--read-only` | Create read-only key | false (read-write) |
| `--repo string` / `-R` | Target repository | current repo |
| `--title string` / `-t` | Title (label) for the deploy key | |

### Delete Deploy Key

```bash
# Delete deploy key by ID
gh secret-kit deploy-key delete 12345

# Delete from specific repository
gh secret-kit deploy-key delete 12345 -R owner/repo
```

| Flag | Description | Default |
| --- | --- | --- |
| `--repo string` / `-R` | Target repository | current repo |

### Get Deploy Key

```bash
# Show deploy key details
gh secret-kit deploy-key get 12345

# Get from specific repository
gh secret-kit deploy-key get 12345 -R owner/repo
```

| Flag | Description | Default |
| --- | --- | --- |
| `--repo string` / `-R` | Target repository | current repo |

### List Deploy Keys

```bash
# List all deploy keys
gh secret-kit deploy-key list

# List for specific repository
gh secret-kit deploy-key list -R owner/repo
```

| Flag | Description | Default |
| --- | --- | --- |
| `--repo string` / `-R` | Target repository | current repo |

### Migrate Deploy Keys

```bash
# Migrate deploy keys to another host
gh secret-kit deploy-key migrate enterprise.internal/owner/repo -R owner/repo

# Exclude keys whose title contains one or more substrings
gh secret-kit deploy-key migrate enterprise.internal/owner/repo -R owner/repo --exclude test,temporary
```

Note: GitHub does not allow the same public key on multiple repositories on the same host. This command is intended for cross-host migration (e.g., github.com to GitHub Enterprise Server).

| Flag | Description | Default |
| --- | --- | --- |
| `--exclude strings` | Exclude keys whose title contains any of the specified substrings (comma-separated or repeated) | |
| `--repo string` / `-R` | Source repository | current repo |

### Deploy Key Setting

```bash
# Get current deploy key setting for an organization
gh secret-kit deploy-key setting myorg

# Enable deploy keys for an organization
gh secret-kit deploy-key setting myorg --set enable

# Disable deploy keys for an organization
gh secret-kit deploy-key setting --owner myorg --set disable
```

| Flag | Description | Default |
| --- | --- | --- |
| `--owner string` | Organization name (alternative to positional argument) | current repo owner |
| `--set string` | Set value: `enable` or `disable` (omit to get) | |

## Environments (gh secret-kit env)

### Copy Environment

```bash
# Copy environment to another repository
gh secret-kit env copy owner/dest-repo --src-env staging

# Copy with different destination environment name
gh secret-kit env copy owner/dest-repo --src-env staging --dst-env production

# Copy to multiple destinations
gh secret-kit env copy owner/repo1 owner/repo2 --src-env staging

# Overwrite existing variables
gh secret-kit env copy owner/dest-repo --src-env staging --overwrite
```

Copies settings, deployment branch policies, and variables. Secrets cannot be copied (use `migrate` instead).

| Flag | Description | Default |
| --- | --- | --- |
| `--dst-env string` | Destination environment name | same as `--src-env` |
| `--dst-host string` | Host to apply to destinations without one | |
| `--overwrite` | Overwrite existing variables | false |
| `--repo string` / `-R` | Source repository | current repo |
| `--src-env string` | Source environment name (required) | |

### Export Environments

```bash
# Export all environments to stdout (YAML)
gh secret-kit env export

# Export specific environment
gh secret-kit env export --env production

# Export as JSON
gh secret-kit env export --format json

# Export to file
gh secret-kit env export -o environments.yaml
```

| Flag | Description | Default |
| --- | --- | --- |
| `--env string` | Export specific environment only | all |
| `--format string` | Output format: `json` or `yaml` | yaml |
| `--output string` / `-o` | Output file path | stdout |
| `--repo string` / `-R` | Source repository | current repo |

### Get Environment

```bash
# Show environment details
gh secret-kit env get production

# Get from specific repository
gh secret-kit env get staging -R owner/repo
```

| Flag | Description | Default |
| --- | --- | --- |
| `--repo string` / `-R` | Source repository | current repo |

### Import Environments

```bash
# Import from file
gh secret-kit env import environments.yaml

# Import from stdin
cat environments.yaml | gh secret-kit env import -

# Import as JSON
gh secret-kit env import environments.json --format json

# Preview without applying
gh secret-kit env import environments.yaml --dryrun

# Overwrite existing environments
gh secret-kit env import environments.yaml --overwrite

# Import with user mapping for reviewers
gh secret-kit env import environments.yaml --usermap usermap.csv
```

| Flag | Description | Default |
| --- | --- | --- |
| `--dryrun` / `-n` | Preview without applying | false |
| `--env string` | Filter by environment name | |
| `--format string` | Input format: `json` or `yaml` | yaml |
| `--overwrite` | Overwrite existing environments | false |
| `--repo string` / `-R` | Destination repository | current repo |
| `--usermap string` | User mapping file for reviewer login conversion | |

### List Environments

```bash
# List all environments
gh secret-kit env list

# List for specific repository
gh secret-kit env list -R owner/repo
```

| Flag | Description | Default |
| --- | --- | --- |
| `--repo string` / `-R` | Target repository | current repo |

### Copy Environment Variables

```bash
# Copy environment variables
gh secret-kit env variable copy owner/dest-repo --src-env staging

# Copy specific variables
gh secret-kit env variable copy owner/dest-repo --src-env staging --variables VAR1,VAR2

# Copy with different destination environment
gh secret-kit env variable copy owner/dest-repo --src-env staging --dst-env production

# Overwrite existing variables
gh secret-kit env variable copy owner/dest-repo --src-env staging --overwrite
```

| Flag | Description | Default |
| --- | --- | --- |
| `--dst-env string` | Destination environment name | same as `--src-env` |
| `--dst-host string` | Host to apply to destinations without one | |
| `--overwrite` | Overwrite existing variables | false |
| `--repo string` / `-R` | Source repository | current repo |
| `--src-env string` | Source environment name (required) | |
| `--variables strings` | Specific variable names (comma-separated or repeated) | all |

## Migration (gh secret-kit migrate)

Since the GitHub API does not expose secret values, migration uses a self-hosted runner to read secrets and set them at the destination via API.

Secret scopes: `repo` (repository), `org` (organization), `env` (environment).

> **Note**: Dependabot secrets are NOT supported.

### Migration Flow

The standard migration flow is: `init` → `create` → `run` → `check` → `delete`.
Use `all` to execute the full pipeline in one command.

1. **init**: Push a stub workflow to a topic branch and open a draft PR
2. **create**: Generate and push the migration workflow
3. **run**: Trigger the workflow by toggling a label on the PR
4. **check**: Verify secrets were migrated
5. **delete**: Close PR and delete the branch

### Runner Setup

```bash
# Start runner listener for an organization (blocks until interrupted)
gh secret-kit migrate runner setup myorg

# Start runner for a specific repository
gh secret-kit migrate runner setup -R owner/repo

# Start with custom label
gh secret-kit migrate runner setup myorg --runner-label custom-label

# Set max concurrent runners
gh secret-kit migrate runner setup myorg --max-runners 4
```

| Flag | Description | Default |
| --- | --- | --- |
| `--max-runners int` | Maximum concurrent runners | 2 |
| `--repo string` / `-R` | Source repository | |
| `--runner-label string` | Custom runner label | gh-secret-kit-migrate |

### Runner Teardown

```bash
# Teardown runner for an organization
gh secret-kit migrate runner teardown myorg

# Teardown runner for a specific repository
gh secret-kit migrate runner teardown -R owner/repo
```

| Flag | Description | Default |
| --- | --- | --- |
| `--repo string` / `-R` | Source repository | |
| `--runner-label string` | Label of the runner to tear down | gh-secret-kit-migrate |

### Runner Prune

```bash
# Remove leftover runners
gh secret-kit migrate runner prune myorg

# Prune all gh-secret-kit runners regardless of label
gh secret-kit migrate runner prune myorg --runner-label ""

# Preview without deleting
gh secret-kit migrate runner prune myorg --dry-run
```

| Flag | Description | Default |
| --- | --- | --- |
| `--dry-run` / `-n` | Preview without deleting | false |
| `--repo string` / `-R` | Source repository | |
| `--runner-label string` | Filter by label (empty = all gh-secret-kit runners) | gh-secret-kit-migrate |

### Migrate Repo Secrets

#### migrate repo all

```bash
# Full pipeline: init → create → run → check → delete
gh secret-kit migrate repo all -s owner/source -d owner/dest

# Migrate specific secrets with overwrite
gh secret-kit migrate repo all -s owner/source -d owner/dest \
  --secrets API_KEY,DB_PASSWORD --overwrite

# Migrate with rename
gh secret-kit migrate repo all -s owner/source -d owner/dest \
  --rename OLD_NAME=NEW_NAME
```

#### migrate repo check

```bash
# Verify repo secret migration
gh secret-kit migrate repo check -s owner/source -d owner/dest

# Check with rename mapping
gh secret-kit migrate repo check -s owner/source -d owner/dest \
  --rename OLD_NAME=NEW_NAME
```

#### migrate repo init / create / run / delete

```bash
# Step-by-step migration
gh secret-kit migrate repo init -s owner/source
gh secret-kit migrate repo create -s owner/source -d owner/dest
gh secret-kit migrate repo run -s owner/source --wait
gh secret-kit migrate repo delete -s owner/source
```

| Flag | Description | Default |
| --- | --- | --- |
| `--branch string` | Topic branch name | gh-secret-kit-migrate |
| `--dst string` / `-d` | Destination repository (`owner/repo` or `HOST/OWNER/REPO`) | |
| `--exclude-secrets strings` | Secret names to exclude | |
| `--label string` | Label name for triggering | gh-secret-kit-migrate |
| `--overwrite` | Overwrite existing secrets | false |
| `--rename strings` | `OLD_NAME=NEW_NAME` mapping (repeatable) | |
| `--runner-label string` | Runner label for the workflow | gh-secret-kit-migrate |
| `--secrets strings` | Specific secret names (comma-separated or repeated) | all |
| `--src string` / `-s` | Source repository | current repo |
| `--timeout string` | Wait timeout (e.g., 5m, 1h) | 10m |
| `--unarchive` | Temporarily unarchive if archived | false |
| `--wait` / `-w` | Wait for workflow completion (run only) | false |
| `--workflow-name string` | Workflow file name | gh-secret-kit-migrate |

### Migrate Org Secrets

#### migrate org all

```bash
# Full pipeline for organization secrets
gh secret-kit migrate org all -s owner/source-repo -d dest-org

# Migrate specific org secrets
gh secret-kit migrate org all -s owner/source-repo -d dest-org \
  --secrets ORG_SECRET_1,ORG_SECRET_2
```

#### migrate org check

```bash
# Verify org secret migration
gh secret-kit migrate org check -s source-org -d dest-org
```

#### migrate org init / create / run / delete

```bash
gh secret-kit migrate org init -s owner/source-repo
gh secret-kit migrate org create -s owner/source-repo -d dest-org
gh secret-kit migrate org run -s owner/source-repo --wait
gh secret-kit migrate org delete -s owner/source-repo
```

Same flags as `migrate repo` except `--dst` accepts an organization name instead of a repository.

### Migrate Env Secrets

#### migrate env all

```bash
# Full pipeline for environment secrets
gh secret-kit migrate env all -s owner/repo -d owner/dest-repo \
  --src-env staging --dst-env production

# Migrate specific environment secrets
gh secret-kit migrate env all -s owner/repo -d owner/dest-repo \
  --src-env staging --dst-env production \
  --secrets API_KEY,DB_PASSWORD
```

#### migrate env check

```bash
# Verify environment secret migration
gh secret-kit migrate env check -s owner/repo -d owner/dest-repo \
  --src-env staging --dst-env production
```

#### migrate env init / create / run / delete

```bash
gh secret-kit migrate env init -s owner/repo
gh secret-kit migrate env create -s owner/repo -d owner/dest-repo \
  --src-env staging --dst-env production
gh secret-kit migrate env run -s owner/repo --wait
gh secret-kit migrate env delete -s owner/repo
```

Same flags as `migrate repo` plus:

| Flag | Description | Default |
| --- | --- | --- |
| `--dst-env string` | Destination environment name (required) | |
| `--src-env string` | Source environment name (required) | |

### Migrate Check (Org-Wide)

```bash
# Scan and verify all migration status for an organization
gh secret-kit migrate check source-org -d dest-org
```

Checks repository secrets, environment secrets, and organization secrets across all matching repositories.

| Flag | Description | Default |
| --- | --- | --- |
| `--dst string` / `-d` | Destination organization (required) | |

### Migrate List

```bash
# List repos with secrets in an organization
gh secret-kit migrate list myorg

# Check a single repository
gh secret-kit migrate list -R owner/repo
```

| Flag | Description | Default |
| --- | --- | --- |
| `--repo string` / `-R` | Check a single repository (skips org scan) | |

### Migrate Plan

```bash
# Generate migration commands for all repos in an organization
gh secret-kit migrate plan source-org -d dest-org

# Generate with overwrite flag
gh secret-kit migrate plan source-org -d dest-org --overwrite

# Generate with user mapping
gh secret-kit migrate plan source-org -d dest-org --usermap usermap.csv

# Skip deploy key scanning
gh secret-kit migrate plan source-org -d dest-org --no-deploy-keys
```

Outputs all commands needed for a full migration: runner setup, secret migration, variable copies, deploy key migrations, and runner teardown.

| Flag | Description | Default |
| --- | --- | --- |
| `--dst string` / `-d` | Destination organization (required) | |
| `--no-deploy-keys` | Skip deploy key scanning | false |
| `--overwrite` | Add `--overwrite` to generated commands | false |
| `--runner-label string` | Runner label | gh-secret-kit-migrate |
| `--unarchive` | Add `--unarchive` to generated commands | false |
| `--usermap string` | User mapping file for `env export/import` commands | |

## Common Workflows

### Migrate All Repository Secrets

```bash
# Terminal 1: Start runner listener (blocks until interrupted)
gh secret-kit migrate runner setup -R owner/source-repo

# Terminal 2: Run full migration pipeline
gh secret-kit migrate repo all -s owner/source-repo -d owner/dest-repo

# After done, clean up runner (Terminal 1: Ctrl+C, then)
gh secret-kit migrate runner teardown -R owner/source-repo
```

### Org-Wide Migration

```bash
# 1. Generate migration plan
gh secret-kit migrate plan source-org -d dest-org > migrate.sh

# 2. Review and edit the generated script
vim migrate.sh

# 3. Start runner
gh secret-kit migrate runner setup source-org

# 4. Execute migration (from another terminal)
bash migrate.sh

# 5. Verify migration
gh secret-kit migrate check source-org -d dest-org

# 6. Tear down runner
gh secret-kit migrate runner teardown source-org
```

### Copy Environment with Variables

```bash
# Copy entire environment (settings + variables)
gh secret-kit env copy owner/dest-repo --src-env staging --dst-env production

# Or export and import for more control
gh secret-kit env export --env staging -o staging.yaml
gh secret-kit env import staging.yaml -R owner/dest-repo --overwrite
```

### Cross-Host Migration (github.com → GHES)

```bash
# Authenticate to both hosts
gh auth login --hostname github.com
gh auth login --hostname enterprise.internal

# Migrate deploy keys (cross-host only)
gh secret-kit deploy-key migrate enterprise.internal/owner/repo -R owner/repo

# Migrate deploy keys, excluding keys with "test" in the title
gh secret-kit deploy-key migrate enterprise.internal/owner/repo -R owner/repo --exclude test

# Migrate secrets
gh secret-kit migrate repo all \
  -s owner/source-repo \
  -d enterprise.internal/owner/dest-repo
```

## Getting Help

```bash
# General help
gh secret-kit --help

# Command help
gh secret-kit deploy-key --help
gh secret-kit migrate repo all --help
gh secret-kit env export --help
```

## References

- Repository: https://github.com/srz-zumix/gh-secret-kit
- Shell Completion Guide: https://github.com/srz-zumix/go-gh-extension/blob/main/docs/shell-completion.md
- Migration Documentation: https://github.com/srz-zumix/gh-secret-kit/blob/main/docs/migrate.md
