# gh secret-kit migrate - Specification

## Overview

`gh secret-kit migrate` migrates GitHub Actions secrets (key/value) from a source repository/organization to a destination repository/organization/environment.

Since the GitHub API does not expose secret values, this command uses [actions/scaleset](https://github.com/actions/scaleset) to register a self-hosted runner on the source, then dispatches a workflow that reads secret values and sets them directly to the destination via API.

## Scope

The following secret types are supported for migration:

- **Repository secrets**
- **Organization secrets** (including visibility and selected repository settings)
- **Environment secrets**

The secret scope is explicitly determined by the subcommand used: `org`, `repo`, or `env`.

## Migration Directions

| Source | Destination | Supported |
| --- | --- | --- |
| Repository | Same owner / different repository | Yes |
| Repository | Different owner / different repository | Yes |
| Environment | Same repository / different environment | Yes |
| Environment | Different repository / environment | Yes |
| Organization | Different organization | Yes |

> **Note**: Scope changes (e.g., repository → organization, organization → repository) are NOT supported in the initial version.

## Architecture

### Flow

```text
User CLI (local)
  │
  ├─ 1. runner setup          → Register scaleset runner on source repo/org
  ├─ 2. {org,repo,env} init   → Push stub workflow to topic branch, open draft PR
  ├─ 3. {org,repo,env} create → Push migration workflow YAML to topic branch
  ├─ 4. {org,repo,env} run    → Trigger workflow via label on the PR
  ├─ 5. {org,repo,env} check  → Compare secrets between source and destination
  ├─ 6. {org,repo,env} delete → Close PR and remove topic branch
  └─ 7. runner teardown       → Unregister and stop the runner
```

### Security

- Secret values are **never written to files** on the runner filesystem.
- The workflow directly calls the GitHub API (via `gh secret-kit` or GitHub API) to set secrets on the destination.
- The generated workflow and runner are cleaned up after migration.

## Command Structure

### Subcommands

`gh secret-kit migrate` has scope-based subcommands with step-level operations for granular control:

| Subcommand | Description |
| --- | --- |
| `gh secret-kit migrate list` | List repositories that have at least one repository secret registered |
| `gh secret-kit migrate runner setup [org]` | Register and start a scaleset runner on the source |
| `gh secret-kit migrate runner teardown [org]` | Unregister and stop the runner |
| `gh secret-kit migrate org all` | Run the full migration pipeline for org secrets (init → create → run → check → delete) |
| `gh secret-kit migrate org init` | Push stub workflow to topic branch and open draft PR (org scope) |
| `gh secret-kit migrate org create` | Generate and push the org secret migration workflow YAML |
| `gh secret-kit migrate org run` | Trigger the org migration workflow via label |
| `gh secret-kit migrate org delete` | Close PR and remove topic branch (org scope) |
| `gh secret-kit migrate org check` | Compare org secrets between source and destination |
| `gh secret-kit migrate repo all` | Run the full migration pipeline for repo secrets (init → create → run → check → delete) |
| `gh secret-kit migrate repo init` | Push stub workflow to topic branch and open draft PR (repo scope) |
| `gh secret-kit migrate repo create` | Generate and push the repo secret migration workflow YAML |
| `gh secret-kit migrate repo run` | Trigger the repo migration workflow via label |
| `gh secret-kit migrate repo delete` | Close PR and remove topic branch (repo scope) |
| `gh secret-kit migrate repo check` | Compare repo secrets between source and destination |
| `gh secret-kit migrate env all` | Run the full migration pipeline for env secrets (init → create → run → check → delete) |
| `gh secret-kit migrate env init` | Push stub workflow to topic branch and open draft PR (env scope) |
| `gh secret-kit migrate env create` | Generate and push the env secret migration workflow YAML |
| `gh secret-kit migrate env run` | Trigger the env migration workflow via label |
| `gh secret-kit migrate env delete` | Close PR and remove topic branch (env scope) |
| `gh secret-kit migrate env check` | Compare env secrets between source and destination |

### Common Options (create)

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--src` | `-s` | string | No | current repository | Source repository (e.g., `owner/repo`; defaults to current repository) |
| `--dst` | `-d` | string | Yes | - | Destination repository or organization |
| `--secrets` | - | []string | No | all | Specific secret names to migrate (comma-separated or repeated flag) |
| `--rename` | - | []string | No | - | Rename mapping in `OLD_NAME=NEW_NAME` format (repeatable) |
| `--overwrite` | - | bool | No | false | Overwrite existing secrets at the destination (default is skip) |
| `--dst-token` | - | string | No | - | PAT or token for the destination (required if destination is on a different host) |
| `--dst-host` | - | string | No | - | GitHub host for the destination (defaults to source repository host) |
| `--branch` | - | string | No | `gh-secret-kit-migrate` | Topic branch to push the workflow YAML to |
| `--label` | - | string | No | `gh-secret-kit-migrate` | Label name for triggering the migration workflow |
| `--runner-label` | - | string | No | `self-hosted` | Runner label to use in the workflow `runs-on` |
| `--unarchive` | - | bool | No | false | Temporarily unarchive the repository if it is archived |
| `--workflow-name` | - | string | No | `gh-secret-kit-migrate` | Name of the generated workflow file |

#### Environment-specific Options (env create / env check)

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--src-env` | - | string | Yes (*) | - | Source environment name |
| `--dst-env` | - | string | Yes (*) | - | Destination environment name |

> (*) Required for `env create` and `env check` commands.

### Common Options (check)

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--src` | `-s` | string | Varies | current repository | Source repository or organization |
| `--dst` | `-d` | string | Yes | - | Destination repository or organization |
| `--secrets` | - | []string | No | all | Specific secret names to check |
| `--rename` | - | []string | No | - | Rename mappings to apply when comparing |
| `--dst-token` | - | string | No | - | PAT or token for the destination |
| `--dst-host` | - | string | No | - | GitHub host for the destination |

> For `org check`, `--src` is the source organization name. For `repo check` and `env check`, `--src` is the source repository.

### migrate list Options

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--repo` | `-R` | string | No | - | Check a single repository (e.g., owner/repo). When specified, org scan is skipped. |

Positional argument: `[org]` — Organization name to scan (defaults to current repository owner).

### runner setup Options

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--repo` | `-R` | string | No | - | Source repository (owner/repo); when omitted uses the first argument as org or falls back to the current repository |
| `--runner-label` | - | string | No | `gh-secret-kit-migrate` | Custom label for the runner |

Positional argument: `[org]` — Organization name for organization-scoped runner (defaults to current repository owner).

### runner teardown Options

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--repo` | `-R` | string | No | - | Source repository (owner/repo); when omitted uses the first argument as org or falls back to the current repository |
| `--runner-label` | - | string | No | `gh-secret-kit-migrate` | Label of the runner to tear down |

Positional argument: `[org]` — Organization name for organization-scoped runner (defaults to current repository owner).

### init Options (shared by org/repo/env)

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--src` | `-s` | string | No | current repository | Source repository (e.g., owner/repo; defaults to current repository) |
| `--branch` | - | string | No | `gh-secret-kit-migrate` | Branch to push the stub workflow to |
| `--label` | - | string | No | `gh-secret-kit-migrate` | Label name to create for triggering the migration workflow |
| `--unarchive` | - | bool | No | false | Temporarily unarchive the repository if it is archived |
| `--workflow-name` | - | string | No | `gh-secret-kit-migrate` | Name of the generated workflow file |

### run Options (shared by org/repo/env)

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--src` | `-s` | string | No | current repository | Source repository (e.g., owner/repo; defaults to current repository) |
| `--branch` | - | string | No | `gh-secret-kit-migrate` | Branch name for the migration PR |
| `--label` | - | string | No | `gh-secret-kit-migrate` | Label name that triggers the migration workflow |
| `--unarchive` | - | bool | No | false | Temporarily unarchive the repository if it is archived |
| `--workflow-name` | - | string | No | `gh-secret-kit-migrate` | Name of the workflow file |
| `--wait` | `-w` | bool | No | false | Wait for the workflow run to complete |
| `--timeout` | - | string | No | `10m` | Timeout duration when waiting for workflow completion (e.g., 5m, 1h) |

### delete Options (shared by org/repo/env)

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--src` | `-s` | string | No | current repository | Source repository (e.g., owner/repo; defaults to current repository) |
| `--branch` | - | string | No | `gh-secret-kit-migrate` | Branch to delete |
| `--unarchive` | - | bool | No | false | Temporarily unarchive the repository if it is archived |
| `--workflow-name` | - | string | No | `gh-secret-kit-migrate` | Name of the workflow file to delete |

### all Options (shared by org/repo/env)

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--src` | `-s` | string | No | current repository | Source repository (e.g., owner/repo; defaults to current repository) |
| `--dst` | `-d` | string | Yes | - | Destination repository or organization |
| `--secrets` | - | []string | No | all | Specific secret names to migrate (comma-separated or repeated flag) |
| `--rename` | - | []string | No | - | Rename mapping in `OLD_NAME=NEW_NAME` format (repeatable) |
| `--overwrite` | - | bool | No | false | Overwrite existing secrets at the destination |
| `--dst-host` | - | string | No | - | GitHub host for the destination (defaults to source repository host) |
| `--branch` | - | string | No | `gh-secret-kit-migrate` | Branch to push the workflow to |
| `--label` | - | string | No | `gh-secret-kit-migrate` | Label name for triggering the migration workflow |
| `--runner-label` | - | string | No | `gh-secret-kit-migrate` | Runner label for the workflow |
| `--workflow-name` | - | string | No | `gh-secret-kit-migrate` | Name of the generated workflow file |
| `--timeout` | - | string | No | `10m` | Timeout duration when waiting for workflow completion |
| `--unarchive` | - | bool | No | false | Temporarily unarchive the repository if it is archived, then re-archive after completion |

## Detailed Behavior

### 0. List Repositories with Secrets (`migrate list`)

When called without arguments or with an org name:

1. Determine the organization (positional arg or current repository owner).
2. List all repositories in the organization (falls back to user repositories if not an org).
3. For each repository, call the Secrets API to get the repository secret count.
4. Output a table of repositories that have at least one repository secret, with the secret count.

When `--repo` / `-R` is specified:

1. Call the Secrets API for the single repository.
2. Output the repository and its secret count if it has at least one secret.

### 1. Runner Setup (`migrate runner setup`)

1. Create a runner scale set using [actions/scaleset](https://github.com/actions/scaleset).
2. Download the GitHub Actions runner binary for the current platform.
3. Start a message session listener in the foreground.
4. The listener polls for job assignments via `GetMessage`.
5. When a job is assigned, automatically generate a JIT config and start an ephemeral runner.
6. After the job completes, the listener loops and waits for the next job assignment.

The command blocks until interrupted (Ctrl+C). Run the workflow dispatch command from another terminal while this command is running.

### 2. Init (`migrate {org,repo,env} init`)

1. Generate a stub workflow YAML with `pull_request` and `workflow_dispatch` triggers.
2. Create a topic branch (`gh-secret-kit-migrate` by default) from the default branch HEAD.
3. Push the stub YAML to the topic branch with `[ci skip]` in the commit message.
4. Open a draft PR from the topic branch to the default branch (causes GitHub to recognise the workflow).
5. Create the trigger label on the repository.
6. The PR and branch are kept open for later use by `run`.

### 3. Create (`migrate {org,repo,env} create`)

1. Generate a GitHub Actions workflow YAML that:
   - Runs on the self-hosted runner (using the specified label).
   - Uses `secrets.*` context to access each specified secret.
   - Calls the GitHub API to set each secret on the destination.
   - Handles `--rename` mappings for secret name changes.
   - Respects `--overwrite` / skip behavior.
2. Push the workflow file to the existing topic branch (created by `init`).

The scope of secrets is determined by the subcommand:

- `repo create`: migrates repository secrets
- `org create`: migrates organization secrets
- `env create`: migrates environment secrets (requires `--src-env` and `--dst-env`)

#### Generated Workflow Behavior

- The workflow uses `pull_request` trigger with a label filter.
- The destination is embedded in the generated workflow YAML at `create` time.
- For each secret, it:
  1. Reads the value from `${{ secrets.SECRET_NAME }}`.
  2. Determines the destination secret name (applying `--rename` if specified).
  3. If `--overwrite` is false, checks if the destination secret already exists, and skips if so.
  4. Sets the secret value on the destination via GitHub API using the provided `--dst-token`.
     - Repository scope: `gh secret set NAME -R DESTINATION`
     - Organization scope: `gh secret set NAME --org DESTINATION`
     - Environment scope: `gh secret set NAME -R DESTINATION -e ENV`

#### Organization Secret Migration

When migrating organization secrets, the workflow also:

- Reads the source secret's `visibility` (`all`, `private`, `selected`).
- If `selected`, reads the list of selected repositories.
- Sets the same visibility and selected repositories on the destination organization secret.
- For cross-organization migration, maps repository names (not IDs) to find corresponding repositories in the destination organization.

### 4. Run (`migrate {org,repo,env} run`)

1. Remove and re-add the trigger label on the open PR to dispatch the workflow.
2. If `--wait` is true, poll until the workflow run completes.
3. Report success/failure for each secret migration.
4. Return error if the workflow run fails or times out.

### 5. Check (`migrate {org,repo,env} check`)

1. List secrets from the source (repo, org, or environment depending on scope).
2. For each secret, apply any `--rename` mappings to determine the expected destination name.
3. List secrets from the destination.
4. Compare and report which secrets exist at the destination and which are missing.
5. Exit with non-zero status if any secrets have not been migrated yet.

### 6. Delete (`migrate {org,repo,env} delete`)

1. Close any open pull requests from the migration topic branch.
2. Delete the topic branch (which removes the migration workflow).

### 7. Runner Teardown (`migrate runner teardown`)

1. Delete the runner scale set from the source repository/organization.
2. Clean up local runner files and state.

## Error Handling

| Scenario | Behavior |
| --- | --- |
| Runner fails to register | Error with registration details |
| Runner not online | Retry with timeout, then error |
| Workflow dispatch fails | Error with workflow dispatch details |
| Workflow run times out | Error with timeout details and partial results |
| Destination secret already exists (no `--overwrite`) | Skip with warning log |
| Destination token lacks permissions | Error with required permissions |
| Secret name in `--secrets` not found on source | Warning log, continue with remaining |
| Cross-org repository mapping fails (for org secret visibility) | Warning log, set visibility to `private` as fallback |

## Examples

### Migrate all repository secrets between repos (same owner)

```sh
# Terminal 1: Start listener (blocks until interrupted)
gh secret-kit migrate runner setup -R owner/source-repo

# Terminal 2: Init, create, run, and clean up
gh secret-kit migrate repo init -s owner/source-repo
gh secret-kit migrate repo create -s owner/source-repo -d owner/dest-repo
gh secret-kit migrate repo run -s owner/source-repo
gh secret-kit migrate repo delete -s owner/source-repo

# After done (Terminal 1), clean up runner
gh secret-kit migrate runner teardown -R owner/source-repo
```

### Migrate all repository secrets (source defaults to current repo)

```sh
# Terminal 1: Start listener (blocks until interrupted)
gh secret-kit migrate runner setup

# Terminal 2: Init, create, run, and clean up
gh secret-kit migrate repo init
gh secret-kit migrate repo create -d owner/dest-repo
gh secret-kit migrate repo run
gh secret-kit migrate repo delete

# After done (Terminal 1), clean up runner
gh secret-kit migrate runner teardown
```

### Migrate organization secrets to another organization

```sh
# Terminal 1: Start listener
gh secret-kit migrate runner setup -R org/some-repo

# Terminal 2: Init, create, run, and clean up
gh secret-kit migrate org init -s org/some-repo
gh secret-kit migrate org create -s org/some-repo -d dest-org
gh secret-kit migrate org run -s org/some-repo
gh secret-kit migrate org delete -s org/some-repo

# After done (Terminal 1), clean up runner
gh secret-kit migrate runner teardown -R org/some-repo
```

### Migrate specific secrets with rename

```sh
gh secret-kit migrate repo create \
  -s owner/source-repo \
  -d owner2/dest-repo \
  --dst-token ghp_xxx \
  --secrets API_KEY,DB_PASSWORD \
  --rename API_KEY=PROD_API_KEY \
  --overwrite
```

### Migrate environment secrets

```sh
gh secret-kit migrate env create \
  -s owner/repo \
  -d owner/repo \
  --src-env staging \
  --dst-env production \
  --secrets API_KEY
```

### Check migration status

```sh
# Check repo secrets
gh secret-kit migrate repo check -s owner/source-repo -d owner/dest-repo

# Check org secrets
gh secret-kit migrate org check -s source-org -d dest-org

# Check env secrets
gh secret-kit migrate env check \
  -s owner/repo -d owner/repo \
  --src-env staging --dst-env production
```

## Open Questions / Future Considerations

- [x] Combined "run all" command design (single command that executes all steps sequentially) — **Implemented as `all` subcommand**
- [ ] Support for Dependabot secrets
- [ ] Support for Codespaces secrets
- [ ] Parallel migration of multiple secrets for performance
- [ ] Interactive mode (prompt user to select secrets from a list)
- [ ] Resume/retry capability for partial failures
- [x] ~~Scope change migration (repo ↔ org)~~ — Scope is explicitly determined by the subcommand used (`org`, `repo`, `env`)
