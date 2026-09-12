# Manage Secrets

Copy GitHub Actions, Copilot coding agent, Codespaces, and Dependabot secrets to other repositories and review their change history.

```sh
gh secret-kit secret [command]
```

## secret agents copy

```sh
gh secret-kit secret agents copy <dst> [dst...] [flags]
```

Copy all (or specific) GitHub Copilot coding agent (Agents) secrets from a source repository to one or more destinations. The `--scope` flag selects which secrets are copied and at which level they are written: `repo` for repository Agents secrets of `--repo`, and `org` for organization Agents secrets of the source owner.

Agents secret values are not readable through the GitHub API; they are only exposed as environment variables inside the Copilot coding agent environment, and only Agents secrets are exposed there. Copilot also only runs the setup steps workflow from the default branch. The command therefore creates a temporary branch carrying `.github/workflows/copilot-setup-steps.yml`, makes it the default branch, registers the destination tokens as temporary Agents secrets, starts a Copilot coding agent session, and lets the setup steps perform the copy. The secret values never reach the local machine, and every temporary change is reverted once the copy finishes.

Each destination argument is `[host/]owner/repo`, or `[host/]org` when `--scope` is `org`. Destinations without a host use the source host. Existing secrets at the destination are skipped unless `--overwrite` is set. Use `--dst-app` to write the values to a different destination secret store.

> **Note**: The source must be on github.com because the Copilot coding agent is not available on GitHub Enterprise Server, the Copilot coding agent must be enabled for the source repository and the authenticated user needs a Copilot license, admin permission is required because the default branch is temporarily changed, and the destination host must be reachable from the agent environment. The pull request the agent opens is based on the temporary branch and is closed when that branch is deleted during the cleanup.

**Arguments:**

- `<dst> [dst...]`: One or more destination repositories, or organizations when `--scope` is `org` (required)

**Options:**

- `--branch string`: Temporary branch made the default branch while the copy runs (defaults to a unique name derived from the workflow run ID, or a timestamp outside GitHub Actions)
- `--dst-app string`: Destination secret store: `actions`, `agents`, `codespaces`, or `dependabot` (default: `agents`)
- `--dst-token string`: PAT or token for the destination host (defaults to the local `gh` authentication; cannot be used when the destinations span multiple hosts)
- `--exclude-secrets strings`: Secret names to exclude from the copy (comma-separated or repeated flag)
- `--keep-workflow`: Keep the temporary branch and Agents secrets after the copy instead of removing them (default: false)
- `--overwrite`: Overwrite existing secrets at destination (default: false)
- `--prompt string`: Task description passed to the Copilot coding agent (defaults to a prompt that tells the agent there is nothing to do)
- `--rename strings`: Rename mapping in `OLD_NAME=NEW_NAME` format (repeatable)
- `--repo string` / `-R`: Source repository (e.g., `owner/repo`; defaults to current repository)
- `--scope string`: Secret scope to copy: `repo` or `org` (default: `repo`)
- `--secrets strings`: Specific secret names to copy (comma-separated or repeated flag; defaults to all)
- `--timeout string`: How long to wait for the agent environment to run the copy (e.g., `30m`, `1h`) (default: `30m`)
- `--token-secret-name string`: Base name of the temporary Agents secret holding the destination token (default: `GH_SECRET_KIT_COPY_TOKEN`)

## secret codespaces copy

```sh
gh secret-kit secret codespaces copy <dst> [dst...] [flags]
```

Copy all (or specific) GitHub Codespaces development environment secrets from a source repository to one or more destinations. The `--scope` flag selects which secrets are copied and at which level they are written: `repo` for repository Codespaces secrets of `--repo`, and `org` for organization Codespaces secrets of the source owner. Add `--include-user-secrets` to also copy the Codespaces secrets of the authenticated user that the source repository has access to.

Codespaces secret values are not readable through the GitHub API; they are only exposed as environment variables inside a running codespace. The command therefore creates an ephemeral codespace on the source repository, copies only the destination tokens into it, runs the copy from within it, and deletes it afterwards. The secret values never reach the local machine.

Each destination argument is `[host/]owner/repo`, or `[host/]org` when `--scope` is `org`. Destinations without a host use the source host. Existing secrets at the destination are skipped unless `--overwrite` is set. Use `--dst-app` to write the values to a different destination secret store.

> **Note**: The source must be on github.com because Codespaces is not available on GitHub Enterprise Server, the `gh` authentication must have the `codespace` scope (`gh auth refresh -s codespace`), creating a codespace consumes Codespaces compute and storage quota, and the destination host must be reachable from the codespace.

**Arguments:**

- `<dst> [dst...]`: One or more destination repositories, or organizations when `--scope` is `org` (required)

**Options:**

- `--branch string`: Source repository branch the codespace is created from (defaults to the default branch)
- `--devcontainer-path string`: Path to the `devcontainer.json` used for the codespace (defaults to the repository default)
- `--dst-app string`: Destination secret store: `actions`, `agents`, `codespaces`, or `dependabot` (default: `codespaces`)
- `--dst-token string`: PAT or token for the destination host (defaults to the local `gh` authentication; cannot be used when the destinations span multiple hosts)
- `--exclude-secrets strings`: Secret names to exclude from the copy (comma-separated or repeated flag)
- `--idle-timeout string`: Allowed inactivity before the codespace is stopped (e.g., `5m`, `1h`) (default: `5m`)
- `--include-user-secrets`: Also copy the Codespaces secrets of the authenticated user (default: false)
- `--keep-codespace`: Keep the codespace after the copy instead of deleting it (default: false)
- `--machine string`: Machine type of the codespace (defaults to the smallest machine type available for the source repository)
- `--overwrite`: Overwrite existing secrets at destination (default: false)
- `--rename strings`: Rename mapping in `OLD_NAME=NEW_NAME` format (repeatable)
- `--repo string` / `-R`: Source repository (e.g., `owner/repo`; defaults to current repository)
- `--retention-period string`: Allowed time after shutting down before the codespace is deleted (e.g., `1h`, `72h`) (default: `1h`)
- `--scope string`: Secret scope to copy: `repo` or `org` (default: `repo`)
- `--secrets strings`: Specific secret names to copy (comma-separated or repeated flag; defaults to all)
- `--token-env-name string`: Base name of the environment variable holding the destination token inside the codespace (default: `GH_SECRET_KIT_COPY_TOKEN`)

## secret copy

```sh
gh secret-kit secret copy <dst> [dst...] [flags]
```

Copy all (or specific) GitHub Actions secrets from a source repository to one or more destinations. The `--scope` flag selects which secrets are copied: `repo` for repository secrets of `--repo`, `org` for organization secrets visible to `--repo`, and `env` for environment secrets of `--src-env`.

Since the GitHub API does not expose secret values, the copy is performed by a workflow generated in the source repository and triggered via `workflow_dispatch`. The token for each destination host is taken from the local `gh` authentication (or from `--dst-token`) and registered as a temporary source repository secret, so a GitHub-hosted runner can reach the destination and no self-hosted runner is required.

> **Note**: The destination host must be reachable from the runner. When the destination is a GitHub Enterprise Server instance that is not reachable from GitHub-hosted runners, use `gh secret-kit migrate` with a self-hosted runner instead.

Each destination argument is `[host/]owner/repo`, or `[host/]org` when `--scope` is `org`. Destinations without a host use the source host. Existing secrets at the destination are skipped unless `--overwrite` is set.

The `--dst-app` flag selects which secret store the destination secrets are written to: `actions` for GitHub Actions secrets, `agents` for Copilot cloud agent (Agents) secrets, `codespaces` for Codespaces secrets, and `dependabot` for Dependabot secrets. Since only Actions secrets are readable from a workflow, `--dst-app` changes the destination store only; the source is always read as Actions secrets. `--dst-env` cannot be combined with a `--dst-app` other than `actions` because those stores have no environment level.

The command waits for the generated workflow run to finish, then deletes the temporary branch, the temporary token secrets, and the workflow run history.

**Arguments:**

- `<dst> [dst...]`: One or more destination repositories, or organizations when `--scope` is `org` (required)

**Options:**

- `--branch string`: Temporary branch name (defaults to a unique name derived from the workflow run ID, or a timestamp outside GitHub Actions)
- `--dst-app string`: Destination secret store: `actions`, `agents`, `codespaces`, or `dependabot` (default: `actions`)
- `--dst-env string`: Destination environment name (defaults to `--src-env` when `--scope` is `env`; cannot be used with a non-`actions` `--dst-app`)
- `--dst-token string`: PAT or token for the destination host (defaults to the local `gh` authentication; cannot be used when the destinations span multiple hosts)
- `--exclude-secrets strings`: Secret names to exclude from the copy (comma-separated or repeated flag)
- `--overwrite`: Overwrite existing secrets at destination (default: false)
- `--rename strings`: Rename mapping in `OLD_NAME=NEW_NAME` format (repeatable)
- `--repo string` / `-R`: Source repository (e.g., `owner/repo`; defaults to current repository)
- `--runner-label string`: Runner label for `runs-on` of the generated workflow (default: `ubuntu-latest`)
- `--scope string`: Secret scope to copy: `repo`, `org`, or `env` (default: `repo`)
- `--secrets strings`: Specific secret names to copy (comma-separated or repeated flag; defaults to all)
- `--src-env string`: Source environment name (required with `--scope env`)
- `--timeout string`: Timeout duration when waiting for workflow completion (e.g., `5m`, `1h`) (default: `10m`)
- `--token-secret-name string`: Base name of the temporary source repository secret holding the destination token (default: `GH_SECRET_KIT_COPY_TOKEN`)
- `--unarchive`: Temporarily unarchive the source repository if it is archived, then re-archive after the copy (default: false)
- `--workflow-name string`: Workflow file name (without extension) of the generated workflow (default: `gh-secret-kit-copy`)

## secret dependabot copy

```sh
gh secret-kit secret dependabot copy <dst> [dst...] [flags]
```

Copy all (or selected) Dependabot secrets from a source repository to one or more destinations. Use `--scope repo` (the default) for repository Dependabot secrets, or `--scope org` for organization Dependabot secrets shared with the source repository. Organization scope requires organization admin access and cannot copy secrets that the source repository cannot access.

Dependabot secret values cannot be read through the GitHub API. The command creates a temporary branch carrying a copy workflow and a separate, intentionally outdated `actions/checkout@v1` reference as bait for a Dependabot update. It makes the branch the default, registers destination tokens as temporary Dependabot secrets, and commits a temporary Dependabot configuration. Dependabot's subsequent push triggers the copy workflow with access to Dependabot secrets; the copy is not triggered by a user or a pull request event. The outdated action is only update bait, not a step in the copy job.

The generated copy workflow has one self-contained Bash script step that uses `gh secret set` to encrypt and write values through the GitHub API, without a checkout or a separate setup step. Source secret values never reach the local machine. The local command uses the GitHub API directly to list source secret names and register or delete temporary Dependabot token secrets; destination tokens are encrypted in memory, without plaintext temporary token files. Success requires both a completed workflow run with the `success` conclusion and the copy script's actual completion marker in the job output; echoed script text is not a completion marker.

Each required destination argument is `[host/]owner/repo`, or `[host/]org` when `--scope org` is selected. Destinations without a host use the source host. `--dst-app` selects the destination secret store, not the source store. Existing destination secrets are skipped unless `--overwrite` is set; absent or empty source values are also skipped. A successful run therefore does not imply that every requested name was written.

After the copy, the command restores the original default branch, closes associated pull requests, and removes temporary Dependabot token secrets, temporary branches, and workflow run history belonging to this invocation. It does not delete unrelated runs merely because they use the same workflow name. `--keep-workflow` retains those temporary resources for inspection, including secrets and run history, but still restores the original default branch.

Ctrl-C and SIGTERM request cancellation and allow cleanup to run. A process crash or forced termination such as SIGKILL cannot perform cleanup automatically.

> **Requirements**: The source repository needs admin permission, Dependabot version updates, and GitHub Actions. The destination host must be reachable from the configured runner, and the destination token must be allowed to write the selected secret store. Custom runners need Bash and GitHub CLI installed. Dependabot has no API for triggering an update check; GitHub schedules the check after the configuration change, usually within several minutes but without a guaranteed delay. Increase `--timeout` if needed.

**Arguments:**

- `<dst> [dst...]`: One or more destination repositories, or organizations with `--scope org` (at least one required; additional destinations optional; no default)

**Examples:**

```sh
# Copy repository Dependabot secrets to two repositories
gh secret-kit secret dependabot copy -R owner/source-repo owner/repo1 owner/repo2

# Copy shared organization Dependabot secrets into another organization's Actions store
gh secret-kit secret dependabot copy -R source-org/source-repo \
  --scope org --dst-app actions dest-org

# Select and rename a secret, using a custom runner and workflow
gh secret-kit secret dependabot copy -R owner/source-repo \
  --secrets API_KEY --rename API_KEY=PROD_API_KEY --overwrite \
  --runner-label self-hosted --workflow-name dependabot-copy --timeout 1h owner/dest-repo
```

**Options (all optional):**

- `--branch string`: Temporary branch made the default branch while copying (defaults to a unique generated name prefixed with `gh-secret-kit-dependabot-copy`)
- `--dst-app string`: Destination secret store: `actions`, `agents`, `codespaces`, or `dependabot` (default: `dependabot`)
- `--dst-token string`: PAT or token for the destination host (defaults to local `gh` authentication; cannot be used when destinations span multiple hosts)
- `--exclude-secrets strings`: Secret names to exclude (comma-separated or repeated flag; default: none)
- `--keep-workflow`: Keep temporary branches, Dependabot secrets, associated pull requests, and run history; still restore the default branch (default: false)
- `--overwrite`: Overwrite existing secrets at destinations (default: false)
- `--rename strings`: Rename mapping in `OLD_NAME=NEW_NAME` format (repeatable; default: no renaming)
- `--repo string` / `-R`: Source repository (e.g., `owner/repo`; defaults to current repository)
- `--runner-label string`: Runner label for `runs-on` of the generated copy workflow (default: `ubuntu-latest`)
- `--scope string`: Secret scope to copy: `repo` or `org` (default: `repo`)
- `--secrets strings`: Specific secret names to copy (comma-separated or repeated flag; defaults to all visible secrets in the selected scope)
- `--timeout string`: Positive duration to wait for Dependabot and the copy workflow to finish (e.g., `30m`, `1h`; default: `30m`)
- `--token-secret-name string`: Base name of temporary Dependabot secrets holding destination tokens; a host suffix is appended (default: `GH_SECRET_KIT_COPY_TOKEN`)
- `--workflow-name string`: Workflow file name without extension for the generated copy workflow (default: `gh-secret-kit-dependabot-copy`)

## secret history

```sh
gh secret-kit secret history [flags]
```

Show the create, update and remove history of secrets from the organization audit log. The audit log API is organization scoped, so the history is read from the organization that owns `--repo` (or from `--owner`), and repository and environment scoped events are filtered by repository. Secret values are never recorded in the audit log; only the secret name and the actor are.

The audit log search accepts a single action per query, so one request is issued for each combination of `--scope`, `--secret-type` and operation, and the results are merged, sorted and truncated to `--limit`.

> **Note**: This command requires GitHub Enterprise Cloud or GitHub Enterprise Server, organization owner permission, and a token with the `read:audit_log` scope. The audit log retains events for a limited period (180 days on GitHub Enterprise Cloud).

**Options:**

- `--env string`: Environment name to filter environment secret events by (defaults to all environments)
- `--format string`: Output format: `json` (defaults to a table)
- `--jq expression` / `-q`: Filter JSON output using a jq expression (requires `--format json`)
- `--limit int`: Maximum number of events to show; 0 or negative for unlimited (default: `100`)
- `--order string`: Sort order of the events: `asc` or `desc` (default: `desc`)
- `--owner string`: Organization/owner to show the secret history for, without filtering by repository. Mutually exclusive with `--repo`
- `--repo string` / `-R`: Repository to show the secret history for (e.g., `owner/repo`; defaults to current repository). Mutually exclusive with `--owner`
- `--scope strings`: Secret scopes to show: `repo`, `org`, or `environment` (comma-separated or repeated flag; defaults to all)
- `--secret string`: Secret name to filter events by (defaults to all secrets)
- `--secret-type strings`: Secret stores to show: `actions`, `dependabot`, or `codespaces` (comma-separated or repeated flag; defaults to all)
- `--since time`: Show events created on or after this date (`YYYY-MM-DD` or RFC3339)
- `--template string` / `-t`: Format JSON output using a Go template (requires `--format json`)
- `--until time`: Show events created on or before this date (`YYYY-MM-DD` or RFC3339)
