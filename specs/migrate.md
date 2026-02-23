# gh secret-kit migrate - Specification

## Overview

`gh secret-kit migrate` migrates GitHub Actions secrets (key/value) from a source repository/organization to a destination repository/organization/environment.

Since the GitHub API does not expose secret values, this command uses [actions/scaleset](https://github.com/actions/scaleset) to register a self-hosted runner on the source, then dispatches a workflow that reads secret values and sets them directly to the destination via API.

## Scope

The following secret types are supported for migration:

- **Repository secrets**
- **Organization secrets** (including visibility and selected repository settings)
- **Environment secrets**

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
  ├─ 1. runner setup   → Register scaleset runner on source repo/org
  ├─ 2. workflow create → Push workflow YAML to source repo
  ├─ 3. workflow run    → Dispatch workflow (runner reads secrets, sets them to destination via API)
  ├─ 4. workflow delete → Remove workflow YAML from source repo
  └─ 5. runner teardown → Unregister and stop the runner
```

### Security

- Secret values are **never written to files** on the runner filesystem.
- The workflow directly calls the GitHub API (via `gh secret-kit` or GitHub API) to set secrets on the destination.
- The generated workflow and runner are cleaned up after migration.

## Command Structure

### Subcommands

`gh secret-kit migrate` has step-level subcommands for granular control:

| Subcommand | Description |
| --- | --- |
| `gh secret-kit migrate runner setup` | Register and start a scaleset runner on the source |
| `gh secret-kit migrate runner teardown` | Unregister and stop the runner |
| `gh secret-kit migrate workflow create` | Generate and push the migration workflow YAML to the source repo |
| `gh secret-kit migrate workflow run` | Dispatch the migration workflow |
| `gh secret-kit migrate workflow delete` | Remove the migration workflow YAML from the source repo |

> A combined "run all steps" command may be added in a future iteration.

### Common Options

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--source` | `-s` | string | Yes | - | Source repository or organization (e.g., `owner/repo` or `org`) |
| `--destination` | `-d` | string | Yes | - | Destination repository or organization (e.g., `owner2/repo2` or `org2`) |
| `--source-env` | - | string | No | - | Source environment name (for environment secrets) |
| `--destination-env` | - | string | No | - | Destination environment name (for environment secrets) |
| `--secrets` | - | []string | No | all | Specific secret names to migrate (comma-separated or repeated flag) |
| `--rename` | - | []string | No | - | Rename mapping in `OLD_NAME=NEW_NAME` format (repeatable) |
| `--overwrite` | - | bool | No | false | Overwrite existing secrets at the destination (default is skip) |
| `--destination-token` | - | string | No | - | PAT or token for the destination (required if destination is a different owner/org) |

### runner setup Options

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--runner-label` | - | string | No | `gh-secret-kit-migrate` | Custom label for the runner |
| `--existing-runner` | - | bool | No | false | Use an existing self-hosted runner instead of setting up a new one |

### runner teardown Options

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--runner-label` | - | string | No | `gh-secret-kit-migrate` | Label of the runner to tear down |

### workflow create Options

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--runner-label` | - | string | No | `gh-secret-kit-migrate` | Runner label to use in the workflow `runs-on` |
| `--workflow-name` | - | string | No | `gh-secret-kit-migrate` | Name of the generated workflow file |
| `--branch` | - | string | No | default branch | Branch to push the workflow YAML to |

### workflow run Options

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--workflow-name` | - | string | No | `gh-secret-kit-migrate` | Name of the workflow to dispatch |
| `--wait` | - | bool | No | true | Wait for the workflow run to complete |
| `--timeout` | - | duration | No | `10m` | Timeout for waiting for the workflow run |

### workflow delete Options

| Option | Short | Type | Required | Default | Description |
| --- | --- | --- | --- | --- | --- |
| `--workflow-name` | - | string | No | `gh-secret-kit-migrate` | Name of the workflow file to delete |
| `--branch` | - | string | No | default branch | Branch to delete the workflow YAML from |

## Detailed Behavior

### 1. Runner Setup (`migrate runner setup`)

1. Download and configure [actions/scaleset](https://github.com/actions/scaleset) runner.
2. Register the runner to the source repository/organization with the specified label.
3. Start the runner process (background).
4. Verify the runner is online and ready to accept jobs.

If `--existing-runner` is specified, skip steps 1-3 and only verify connectivity using the specified `--runner-label`.

### 2. Workflow Create (`migrate workflow create`)

1. Generate a GitHub Actions workflow YAML that:
   - Runs on the self-hosted runner (using the specified label).
   - Uses `secrets.*` context to access each specified secret.
   - Calls the GitHub API (or `gh secret-kit` CLI) to set each secret on the destination.
   - Handles `--rename` mappings for secret name changes.
   - Respects `--overwrite` / skip behavior.
2. Push the workflow file to the source repository (on the specified branch).

#### Generated Workflow Behavior

- The workflow uses `workflow_dispatch` trigger.
- For each secret, it:
  1. Reads the value from `${{ secrets.SECRET_NAME }}`.
  2. Determines the destination secret name (applying `--rename` if specified).
  3. If `--overwrite` is false, checks if the destination secret already exists, and skips if so.
  4. Sets the secret value on the destination via GitHub API using the provided `--destination-token`.

#### Organization Secret Migration

When migrating organization secrets, the workflow also:

- Reads the source secret's `visibility` (`all`, `private`, `selected`).
- If `selected`, reads the list of selected repositories.
- Sets the same visibility and selected repositories on the destination organization secret.
- For cross-organization migration, maps repository names (not IDs) to find corresponding repositories in the destination organization.

### 3. Workflow Run (`migrate workflow run`)

1. Dispatch the workflow via `workflow_dispatch` event.
2. If `--wait` is true (default), poll until the workflow run completes.
3. Report success/failure for each secret migration.
4. Return error if the workflow run fails or times out.

### 4. Workflow Delete (`migrate workflow delete`)

1. Delete the workflow YAML file from the source repository.
2. Optionally clean up any workflow run artifacts.

### 5. Runner Teardown (`migrate runner teardown`)

1. Stop the runner process.
2. Unregister the runner from the source repository/organization.
3. Clean up local runner files.

Skipped if `--existing-runner` was used during setup.

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
gh secret-kit migrate runner setup --source owner/source-repo
gh secret-kit migrate workflow create --source owner/source-repo --destination owner/dest-repo
gh secret-kit migrate workflow run --source owner/source-repo --destination owner/dest-repo
gh secret-kit migrate workflow delete --source owner/source-repo
gh secret-kit migrate runner teardown --source owner/source-repo
```

### Migrate specific secrets with rename

```sh
gh secret-kit migrate workflow create \
  --source owner/source-repo \
  --destination owner2/dest-repo \
  --destination-token ghp_xxx \
  --secrets API_KEY,DB_PASSWORD \
  --rename API_KEY=PROD_API_KEY \
  --overwrite
```

### Migrate environment secrets

```sh
gh secret-kit migrate workflow create \
  --source owner/repo \
  --destination owner/repo \
  --source-env staging \
  --destination-env production \
  --secrets API_KEY
```

### Use an existing self-hosted runner

```sh
gh secret-kit migrate runner setup \
  --source owner/repo \
  --existing-runner \
  --runner-label my-runner

gh secret-kit migrate workflow create \
  --source owner/repo \
  --destination owner/repo2 \
  --runner-label my-runner
```

## Open Questions / Future Considerations

- [ ] Combined "run all" command design (single command that executes all 5 steps sequentially)
- [ ] Support for Dependabot secrets
- [ ] Support for Codespaces secrets
- [ ] Parallel migration of multiple secrets for performance
- [ ] Interactive mode (prompt user to select secrets from a list)
- [ ] Resume/retry capability for partial failures
- [ ] Scope change migration (repo ↔ org)
