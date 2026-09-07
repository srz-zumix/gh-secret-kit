# Migrate GitHub Actions Secrets

Migrate GitHub Actions secrets between repositories, organizations, and environments.

```sh
gh secret-kit migrate [command]
```

Since the GitHub API does not expose secret values, this command uses a self-hosted runner to read secret values and set them to the destination via API.

The secret scope is determined by the subcommand: `org` for organization secrets, `repo` for repository secrets, and `env` for environment secrets.

> **Note**: Dependabot secrets are NOT supported. Dependabot secrets can only be accessed by workflows triggered by Dependabot, so user-triggered migration is not possible.

## migrate env

Migrate environment secrets between repositories.

Each subcommand (`all`, `init`, `create`, `run`, `delete`, `check`, `dispatch`) operates on environment-scoped secrets.

## migrate env all

```sh
gh secret-kit migrate env all [flags]
```

Execute all migration steps in sequence: init, create, run, check, and delete.

This command initializes the stub workflow, generates and pushes the migration workflow, triggers it, waits for completion, verifies the results, and cleans up.

**Options:**

- `--branch string`: Branch to push the workflow to (default: "gh-secret-kit-migrate")
- `--dst string` / `-d`: Destination repository (e.g., owner/repo or HOST/OWNER/REPO) (required)
- `--dst-env string`: Destination environment name (required)
- `--dst-token-secret string`: Secret variable name that holds the PAT for the destination (e.g. `DST_PAT`; referenced as `${{ secrets.<name> }}` in the generated workflow)
- `--label string`: Label name for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--overwrite`: Overwrite existing secrets at destination
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--runner-label string`: Runner label for the workflow (default: "gh-secret-kit-migrate")
- `--secrets strings`: Specific secret names to migrate (comma-separated or repeated flag; defaults to all)
- `--exclude-secrets strings`: Secret names to exclude from migration (comma-separated or repeated flag)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--src-env string`: Source environment name (required)
- `--skip-check`: Skip the check step
- `--timeout string`: Timeout duration when waiting for workflow completion (e.g., 5m, 1h) (default: "10m")
- `--unarchive`: Temporarily unarchive the repository if it is archived, then re-archive after completion
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

## migrate env check

```sh
gh secret-kit migrate env check [flags]
```

Compare environment secrets between source and destination repositories. For each secret in the source environment, check whether the corresponding secret (after applying any `--rename` mappings) exists in the destination environment. Exits with a non-zero status if any secrets have not been migrated yet.

**Options:**

- `--dst string` / `-d`: Destination repository (e.g., owner/repo or HOST/OWNER/REPO)
- `--dst-env string`: Destination environment name
- `--dst-token string`: PAT or token for the destination (required if destination is on a different host)
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--secrets strings`: Specific secret names to check (comma-separated or repeated flag; defaults to all)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--src-env string`: Source environment name

## migrate env create

```sh
gh secret-kit migrate env create [flags]
```

Generate a GitHub Actions workflow that migrates environment secrets from the source repository's environment to the destination repository's environment. The workflow is pushed to the source repository on a topic branch.

**Options:**

- `--branch string`: Branch to push the workflow to (default: "gh-secret-kit-migrate")
- `--dst string` / `-d`: Destination repository (e.g., owner/repo or HOST/OWNER/REPO)
- `--dst-env string`: Destination environment name
- `--dst-token-secret string`: Secret variable name that holds the PAT for the destination (e.g. `DST_PAT`; referenced as `${{ secrets.<name> }}` in the generated workflow)
- `--label string`: Label name for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--overwrite`: Overwrite existing secrets at destination
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--runner-label string`: Runner label for the workflow (default: "self-hosted")
- `--secrets strings`: Specific secret names to migrate (comma-separated or repeated flag; defaults to all)
- `--exclude-secrets strings`: Secret names to exclude from migration (comma-separated or repeated flag)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

## migrate env delete

```sh
gh secret-kit migrate env delete [flags]
```

Close any open pull requests from the migration topic branch and then delete the branch. This removes the generated workflow file and all related resources from the source repository.

**Options:**

- `--branch string`: Branch to delete (default: "gh-secret-kit-migrate")
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--workflow-name string`: Name of the workflow file (default: "gh-secret-kit-migrate")

## migrate env dispatch

```sh
gh secret-kit migrate env dispatch [flags]
```

Generate an environment secret migration workflow, push it to a temporary branch, and trigger it via `workflow_dispatch`. This command has two modes. In **self-rewrite** mode (no `--src`), it must be invoked from inside a `workflow_dispatch`-triggered workflow and rewrites the currently running workflow, reusing the same runner setting (unless `--runner-label` is given). In **target-specified** mode (`--src` given), the workflow does not need to run inside a workflow; the workflow is registered in the source repository using a syntax-error workflow trick, then the corrected workflow is pushed and dispatched. In target-specified mode, `--runner-label` is required and `--workflow-name` selects the workflow file name. The temporary branch is deleted by the generated workflow after a successful run.

**Options:**

- `--branch string`: Temporary dispatch branch name (default: unique name derived from the workflow run ID, or a timestamp outside GitHub Actions)
- `--delete-run-after-wait`: Delete the dispatched workflow run's history after it completes successfully (requires `--wait`)
- `--dst string` / `-d`: Destination repository (e.g., owner/repo or HOST/OWNER/REPO) (required)
- `--dst-env string`: Destination environment name (required)
- `--dst-token-secret string`: Secret variable name that holds the PAT for the destination (referenced as `${{ secrets.<name> }}` in the workflow)
- `--exclude-secrets strings`: Secret names to exclude from migration (comma-separated or repeated flag)
- `--overwrite`: Overwrite existing secrets at destination
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--runner-label string`: Runner label for `runs-on` (default: the running workflow's runner setting; required with `--src`)
- `--secrets strings`: Specific secret names to migrate (comma-separated or repeated flag; defaults to all)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to the repository running the workflow; when set, enables target-specified mode)
- `--src-env string`: Source environment name (required)
- `--timeout string`: Timeout duration when waiting for workflow completion (e.g., 5m, 1h) (default: "10m")
- `--unarchive`: Temporarily unarchive the source repository if it is archived, then re-archive after the dispatch
- `--wait` / `-w`: Wait for the dispatched workflow run to complete
- `--workflow-name string`: Workflow file name (without extension) for target-specified mode (default: "gh-secret-kit-migrate")

## migrate env init

```sh
gh secret-kit migrate env init [flags]
```

Push a stub workflow file (with `[ci skip]` in the commit message) to a topic branch, then open a draft PR so GitHub recognises the workflow file. The PR and branch are kept open for later use by `run`. The branch can be cleaned up later with `delete`.

**Options:**

- `--branch string`: Branch to push the stub workflow to (default: "gh-secret-kit-migrate")
- `--label string`: Label name to create for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

## migrate env run

```sh
gh secret-kit migrate env run [flags]
```

Trigger the migration workflow by removing and re-adding the trigger label on the open PR. Optionally wait for the workflow run to complete.

**Options:**

- `--branch string`: Branch name for the migration PR (default: "gh-secret-kit-migrate")
- `--label string`: Label name that triggers the migration workflow (default: "gh-secret-kit-migrate")
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--timeout string`: Timeout duration when waiting for workflow completion (e.g., 5m, 1h) (default: "10m")
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--wait` / `-w`: Wait for the workflow run to complete
- `--workflow-name string`: Name of the workflow file (default: "gh-secret-kit-migrate")

## migrate check

```sh
gh secret-kit migrate check [org] [flags]
```

Scan the source and destination organizations, identify matching repository and environment pairs that have secrets, and run the migration check for each.

This command verifies whether secrets from the source have been successfully migrated to the destination. It checks:

- Repository secrets for all matching repositories
- Environment secrets for all matching environments
- Organization secrets (if any)

Exits with a non-zero status if any secrets have not been migrated yet.

**Arguments:**

- `[org]`: Source organization name (e.g., org or HOST/org). Defaults to current repository owner.

**Options:**

- `--dst string` / `-d`: Destination organization (e.g., org or HOST/org) (required)

## migrate delete-runs

```sh
gh secret-kit migrate delete-runs [flags]
```

Delete completed workflow run history for the given workflow file name. This targets the run history left behind by the `dispatch`/`dump` commands: their per-run dispatch branches self-delete after a successful run, but the run entries themselves remain in the Actions UI until removed. In-progress runs are never deleted.

**Options:**

- `--dryrun` / `-n`: List the runs that would be deleted without deleting them
- `--keep-last int`: Keep the N most recent completed runs and delete the rest (default: 0, deletes all completed runs)
- `--repo string` / `-R`: Source repository (e.g., owner/repo; defaults to current repository)
- `--unarchive`: Temporarily unarchive the repository if it is archived, then re-archive after completion
- `--workflow-name string`: Workflow file name (without extension) to delete run history for (default: "gh-secret-kit-migrate")

## migrate list

```sh
gh secret-kit migrate list [org] [flags]
```

List repositories that have at least one repository secret registered.

When called without arguments, the current repository's owner is used as the organization. You can pass an explicit org name (or HOST/ORG) as the first argument. Use `-R`/`--repo` to check a single specific repository instead of scanning an organization.

**Options:**

- `--repo string` / `-R`: Check a single repository (e.g., owner/repo). When specified, org scan is skipped.

## migrate plan

```sh
gh secret-kit migrate plan [org] [flags]
```

Scan source organization for repositories with secrets, check if matching repositories exist in the destination organization, and output the migration commands for all matching pairs.

This command does not perform any migration; it only outputs the commands that would be needed to migrate secrets from source to destination. Each migration command is preceded by a comment listing the secret names that will be migrated.

For each environment with secrets or variables, the plan also outputs an `env export | env import` pipeline, an `env variable copy` command, and a `migrate env all` command. The output depends on the destination environment state and the flags specified:

- **Destination environment does not exist**: Both `env export | env import` (creates the environment and copies variables) and `migrate env all` are output as executable commands.
- **Destination environment exists, `--overwrite` not set, `--usermap` not set**: `env export | env import` is commented out to avoid overwriting existing settings; `env variable copy` (if the environment has variables) and `migrate env all` are output as executable commands.
- **Destination environment exists, `--overwrite` set**: Both `env export | env import` (with `--overwrite`, handles variables) and `migrate env all` (with `--overwrite`) are output as executable commands.
- **Destination environment exists, `--usermap` set**: Both `env export | env import` (with `--usermap`, handles reviewer mapping and variables) and `migrate env all` are output as executable commands.
- **Environment has required reviewers, `--usermap` not set**: All commands are commented out because reviewer names may not be resolvable in the destination organization. Manual adjustment is required before running.
- **Environment has required reviewers, `--usermap` set**: `env export | env import` (with `--usermap`) and `migrate env all` are output as executable commands.

When the source and destination organizations are on different hosts, deploy key migration commands (`deploy-key migrate`) are also included for each matching repository that has deploy keys. Use `--no-deploy-keys` to skip this extra per-repository API call.

**Arguments:**

- `[org]`: Source organization name (e.g., org or HOST/org). Defaults to current repository owner.

**Options:**

- `--dst string` / `-d`: Destination organization (e.g., org or HOST/org) (required)
- `--extra-deploy-key-options string`: Additional options appended verbatim to generated `deploy-key migrate` commands (default: "")
- `--extra-env-options string`: Additional options appended verbatim to generated `migrate env all` commands (default: "")
- `--extra-org-options string`: Additional options appended verbatim to generated `migrate org all` commands (default: "")
- `--extra-repo-options string`: Additional options appended verbatim to generated `migrate repo all` commands (default: "")
- `--no-deploy-keys`: Skip deploy key scanning (avoids extra API calls per repository) (default: false)
- `--overwrite`: Add `--overwrite` to generated migration and copy commands that support it (default: false)
- `--runner-group string`: Runner group name for the runner setup/teardown commands (default: "")
- `--runner-label string`: Runner label for the workflow (default: "gh-secret-kit-migrate")
- `--unarchive`: Add `--unarchive` to generated migration commands (default: false)
- `--usermap string`: Add `--usermap` to generated `env export | env import` commands and make those pipelines executable even for existing destination environments

## migrate org

Migrate organization secrets between organizations.

Each subcommand (`all`, `init`, `create`, `run`, `delete`, `check`) operates on organization-scoped secrets.

## migrate org all

```sh
gh secret-kit migrate org all [flags]
```

Execute all migration steps in sequence: init, create, run, check, and delete.

This command initializes the stub workflow, generates and pushes the migration workflow, triggers it, waits for completion, verifies the results, and cleans up.

**Options:**

- `--branch string`: Branch to push the workflow to (default: "gh-secret-kit-migrate")
- `--dst string` / `-d`: Destination organization (e.g., org or HOST/org) (required)
- `--dst-token-secret string`: Secret variable name that holds the PAT for the destination (e.g. `DST_PAT`; referenced as `${{ secrets.<name> }}` in the generated workflow)
- `--label string`: Label name for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--overwrite`: Overwrite existing secrets at destination
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--runner-label string`: Runner label for the workflow (default: "gh-secret-kit-migrate")
- `--secrets strings`: Specific secret names to migrate (comma-separated or repeated flag; defaults to all)
- `--exclude-secrets strings`: Secret names to exclude from migration (comma-separated or repeated flag)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--skip-check`: Skip the check step
- `--timeout string`: Timeout duration when waiting for workflow completion (e.g., 5m, 1h) (default: "10m")
- `--unarchive`: Temporarily unarchive the repository if it is archived, then re-archive after completion
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

## migrate org check

```sh
gh secret-kit migrate org check [flags]
```

Compare organization secrets between source and destination organizations. For each secret in the source, check whether the corresponding secret (after applying any `--rename` mappings) exists in the destination. Exits with a non-zero status if any secrets have not been migrated yet.

**Options:**

- `--dst string` / `-d`: Destination organization (e.g., org or HOST/org)
- `--dst-token string`: PAT or token for the destination (required if destination is on a different host)
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--secrets strings`: Specific secret names to check (comma-separated or repeated flag; defaults to all)
- `--src string` / `-s`: Source organization name

## migrate org create

```sh
gh secret-kit migrate org create [flags]
```

Generate a GitHub Actions workflow that migrates organization secrets from the source repository's organization to the destination organization. The workflow is pushed to the source repository on a topic branch.

**Options:**

- `--branch string`: Branch to push the workflow to (default: "gh-secret-kit-migrate")
- `--dst string` / `-d`: Destination organization (e.g., org or HOST/org)
- `--dst-token-secret string`: Secret variable name that holds the PAT for the destination (e.g. `DST_PAT`; referenced as `${{ secrets.<name> }}` in the generated workflow)
- `--label string`: Label name for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--overwrite`: Overwrite existing secrets at destination
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--runner-label string`: Runner label for the workflow (default: "self-hosted")
- `--secrets strings`: Specific secret names to migrate (comma-separated or repeated flag; defaults to all)
- `--exclude-secrets strings`: Secret names to exclude from migration (comma-separated or repeated flag)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

## migrate org delete

```sh
gh secret-kit migrate org delete [flags]
```

Close any open pull requests from the migration topic branch and then delete the branch. This removes the generated workflow file and all related resources from the source repository.

**Options:**

- `--branch string`: Branch to delete (default: "gh-secret-kit-migrate")
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--workflow-name string`: Name of the workflow file (default: "gh-secret-kit-migrate")

## migrate org init

```sh
gh secret-kit migrate org init [flags]
```

Push a stub workflow file (with `[ci skip]` in the commit message) to a topic branch, then open a draft PR so GitHub recognises the workflow file. The PR and branch are kept open for later use by `run`. The branch can be cleaned up later with `delete`.

**Options:**

- `--branch string`: Branch to push the stub workflow to (default: "gh-secret-kit-migrate")
- `--label string`: Label name to create for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

## migrate org run

```sh
gh secret-kit migrate org run [flags]
```

Trigger the migration workflow by removing and re-adding the trigger label on the open PR. Optionally wait for the workflow run to complete.

**Options:**

- `--branch string`: Branch name for the migration PR (default: "gh-secret-kit-migrate")
- `--label string`: Label name that triggers the migration workflow (default: "gh-secret-kit-migrate")
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--timeout string`: Timeout duration when waiting for workflow completion (e.g., 5m, 1h) (default: "10m")
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--wait` / `-w`: Wait for the workflow run to complete
- `--workflow-name string`: Name of the workflow file (default: "gh-secret-kit-migrate")

## migrate repo

Migrate repository secrets between repositories.

Each subcommand (`all`, `init`, `create`, `run`, `delete`, `check`, `dispatch`) operates on repository-scoped secrets.

## migrate repo all

```sh
gh secret-kit migrate repo all [flags]
```

Execute all migration steps in sequence: init, create, run, check, and delete.

This command initializes the stub workflow, generates and pushes the migration workflow, triggers it, waits for completion, verifies the results, and cleans up.

**Options:**

- `--branch string`: Branch to push the workflow to (default: "gh-secret-kit-migrate")
- `--dst string` / `-d`: Destination repository (e.g., owner/repo or HOST/OWNER/REPO) (required)
- `--dst-token-secret string`: Secret variable name that holds the PAT for the destination (e.g. `DST_PAT`; referenced as `${{ secrets.<name> }}` in the generated workflow)
- `--label string`: Label name for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--overwrite`: Overwrite existing secrets at destination
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--runner-label string`: Runner label for the workflow (default: "gh-secret-kit-migrate")
- `--secrets strings`: Specific secret names to migrate (comma-separated or repeated flag; defaults to all)
- `--exclude-secrets strings`: Secret names to exclude from migration (comma-separated or repeated flag)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--skip-check`: Skip the check step
- `--timeout string`: Timeout duration when waiting for workflow completion (e.g., 5m, 1h) (default: "10m")
- `--unarchive`: Temporarily unarchive the repository if it is archived, then re-archive after completion
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

## migrate repo check

```sh
gh secret-kit migrate repo check [flags]
```

Compare repository secrets between source and destination repositories. For each secret in the source, check whether the corresponding secret (after applying any `--rename` mappings) exists in the destination. Exits with a non-zero status if any secrets have not been migrated yet.

**Options:**

- `--dst string` / `-d`: Destination repository (e.g., owner/repo or HOST/OWNER/REPO)
- `--dst-token string`: PAT or token for the destination (required if destination is on a different host)
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--secrets strings`: Specific secret names to check (comma-separated or repeated flag; defaults to all)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)

## migrate repo create

```sh
gh secret-kit migrate repo create [flags]
```

Generate a GitHub Actions workflow that migrates repository secrets from the source repository to the destination repository. The workflow is pushed to the source repository on a topic branch.

**Options:**

- `--branch string`: Branch to push the workflow to (default: "gh-secret-kit-migrate")
- `--dst string` / `-d`: Destination repository (e.g., owner/repo or HOST/OWNER/REPO)
- `--dst-token-secret string`: Secret variable name that holds the PAT for the destination (e.g. `DST_PAT`; referenced as `${{ secrets.<name> }}` in the generated workflow)
- `--label string`: Label name for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--overwrite`: Overwrite existing secrets at destination
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--runner-label string`: Runner label for the workflow (default: "self-hosted")
- `--secrets strings`: Specific secret names to migrate (comma-separated or repeated flag; defaults to all)
- `--exclude-secrets strings`: Secret names to exclude from migration (comma-separated or repeated flag)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

## migrate repo delete

```sh
gh secret-kit migrate repo delete [flags]
```

Close any open pull requests from the migration topic branch and then delete the branch. This removes the generated workflow file and all related resources from the source repository.

**Options:**

- `--branch string`: Branch to delete (default: "gh-secret-kit-migrate")
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--workflow-name string`: Name of the workflow file (default: "gh-secret-kit-migrate")

## migrate repo dispatch

```sh
gh secret-kit migrate repo dispatch [flags]
```

Generate a secret migration workflow, push it to a temporary branch, and trigger it via `workflow_dispatch`. This command has two modes. In **self-rewrite** mode (no `--src`), it must be invoked from inside a `workflow_dispatch`-triggered workflow and rewrites the currently running workflow, reusing the same runner setting (unless `--runner-label` is given). In **target-specified** mode (`--src` given), the workflow does not need to run inside a workflow; the workflow is registered in the source repository using a syntax-error workflow trick, then the corrected workflow is pushed and dispatched. In target-specified mode, `--runner-label` is required and `--workflow-name` selects the workflow file name. The temporary branch is deleted by the generated workflow after a successful run.

**Options:**

- `--branch string`: Temporary dispatch branch name (default: unique name derived from the workflow run ID, or a timestamp outside GitHub Actions)
- `--delete-run-after-wait`: Delete the dispatched workflow run's history after it completes successfully (requires `--wait`)
- `--dst string` / `-d`: Destination repository (e.g., owner/repo or HOST/OWNER/REPO) (required)
- `--dst-token-secret string`: Secret variable name that holds the PAT for the destination (e.g. `DST_PAT`; referenced as `${{ secrets.<name> }}` in the workflow)
- `--exclude-secrets strings`: Secret names to exclude from migration (comma-separated or repeated flag)
- `--overwrite`: Overwrite existing secrets at destination
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--runner-label string`: Runner label for `runs-on` (default: the running workflow's runner setting; required with `--src`)
- `--secrets strings`: Specific secret names to migrate (comma-separated or repeated flag; defaults to all)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to the repository running the workflow; when set, enables target-specified mode)
- `--timeout string`: Timeout duration when waiting for workflow completion (e.g., 5m, 1h) (default: "10m")
- `--unarchive`: Temporarily unarchive the source repository if it is archived, then re-archive after the dispatch
- `--wait` / `-w`: Wait for the dispatched workflow run to complete
- `--workflow-name string`: Workflow file name (without extension) for target-specified mode (default: "gh-secret-kit-migrate")

## migrate repo init

```sh
gh secret-kit migrate repo init [flags]
```

Push a stub workflow file (with `[ci skip]` in the commit message) to a topic branch, then open a draft PR so GitHub recognises the workflow file. The PR and branch are kept open for later use by `run`. The branch can be cleaned up later with `delete`.

**Options:**

- `--branch string`: Branch to push the stub workflow to (default: "gh-secret-kit-migrate")
- `--label string`: Label name to create for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

## migrate repo run

```sh
gh secret-kit migrate repo run [flags]
```

Trigger the migration workflow by removing and re-adding the trigger label on the open PR. Optionally wait for the workflow run to complete.

**Options:**

- `--branch string`: Branch name for the migration PR (default: "gh-secret-kit-migrate")
- `--label string`: Label name that triggers the migration workflow (default: "gh-secret-kit-migrate")
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--timeout string`: Timeout duration when waiting for workflow completion (e.g., 5m, 1h) (default: "10m")
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--wait` / `-w`: Wait for the workflow run to complete
- `--workflow-name string`: Name of the workflow file (default: "gh-secret-kit-migrate")

## migrate runner setup

```sh
gh secret-kit migrate runner setup [[HOST]/ORG] [flags]
```

Register a self-hosted runner and start a message session listener for secret migration.
Creates a runner scale set on the source repository/organization, downloads the runner binary,
and starts a foreground message session listener. The listener waits for job assignments,
automatically starts an ephemeral runner via JIT config when a workflow job is dispatched,
and loops continuously until interrupted. Run the workflow dispatch command from another
terminal while this command is running.
`.gh-secret-kit-state.json` is written to the **current working directory**. Running setup again in
the same directory is rejected if the file already exists. To run multiple concurrent runners,
execute setup from different directories. If the setup process was interrupted, run
`runner restart` from the same directory to reuse the saved state.

**Options:**

- `--max-runners int`: Maximum number of concurrent runners (default: 2)
- `--repo string` / `-R`: Source repository (owner/repo); when omitted uses the first argument as org or falls back to the current repository
- `--runner-group string`: Runner group name to place the scale set in (created automatically if not found; defaults to the default runner group when omitted)
- `--runner-label string`: Custom label for the runner (default: "gh-secret-kit-migrate")

## migrate runner restart

```sh
gh secret-kit migrate runner restart [flags]
```

Restart the self-hosted runner listener from `.gh-secret-kit-state.json` in the
**current working directory**. Reuses the saved runner scale set, runner label,
runner group, and runner directory without creating a new scale set. Use this when
`runner setup` was interrupted and the state file still exists. The source repository
or organization is read from the state file.

**Options:**

- `--max-runners int`: Maximum number of concurrent runners (default: 2)

## migrate runner teardown

```sh
gh secret-kit migrate runner teardown [[HOST]/ORG] [flags]
```

Unregister and stop the self-hosted runner. Reads `.gh-secret-kit-state.json` from the **current
working directory** to determine the runner label, runner group, scale set ID, and runner
directory. If the runner group was created during setup, it is also deleted.
When a source argument is provided, it is validated against the source recorded in the state
file; if they do not match the command aborts without modifying the state file.

**Options:**

- `--repo string` / `-R`: Source repository (owner/repo); validated against state when provided
- `--runner-group string`: Runner group name to search for the scale set (read from state when available; fallback when state file is absent)
- `--runner-label string`: Label of the runner to tear down (read from state when available; default: "gh-secret-kit-migrate")

## migrate runner prune

```sh
gh secret-kit migrate runner prune [[HOST]/ORG] [flags]
```

Remove self-hosted runners whose names start with `gh-secret-kit-` that were left behind by previous runs. Only runners matching `--runner-label` are targeted (pass `--runner-label ""` to match all `gh-secret-kit-` runners). Busy runners are skipped to avoid disrupting running jobs. Use `--dryrun` (`-n`) to preview without deleting.

Use `--runner-group` to restrict pruning to runners belonging to a specific runner group (org-level only; cannot be combined with `--repo`).

**Options:**

- `--dryrun` / `-n`: Print runners that would be removed without deleting them (default: false)
- `--repo string` / `-R`: Source repository (owner/repo); when omitted uses the first argument as org or falls back to the current repository (mutually exclusive with `--runner-group`)
- `--runner-group string`: Only remove runners belonging to this runner group name (org-level only; mutually exclusive with `--repo`)
- `--runner-label string`: Only remove runners that have this label (default: "gh-secret-kit-migrate"; empty string matches all gh-secret-kit runners)
