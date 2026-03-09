# gh-secret-kit

A gh extension for the GitHub Actions secrets API.

## Installation

```sh
gh extension install srz-zumix/gh-secret-kit
```

## Shell Completion

**Workaround Available!** While gh CLI doesn't natively support extension completion, we provide a patch script that enables it.

**Prerequisites:** Before setting up gh-secret-kit completion, ensure gh CLI completion is configured for your shell. See [gh completion documentation](https://cli.github.com/manual/gh_completion) for setup instructions.

For detailed installation instructions and setup for each shell, see the [Shell Completion Guide](https://github.com/srz-zumix/go-gh-extension/blob/main/docs/shell-completion.md).

## Commands

### Migrate GitHub Actions Secrets

Migrate GitHub Actions secrets between repositories, organizations, and environments.

```sh
gh secret-kit migrate [command]
```

Since the GitHub API does not expose secret values, this command uses a self-hosted runner to read secret values and set them to the destination via API.

The secret scope is determined by the subcommand: `org` for organization secrets, `repo` for repository secrets, and `env` for environment secrets.

> **Note**: Dependabot secrets are NOT supported. Dependabot secrets can only be accessed by workflows triggered by Dependabot, so user-triggered migration is not possible.

#### migrate env

Migrate environment secrets between repositories.

Each subcommand (`all`, `init`, `create`, `run`, `delete`, `check`) operates on environment-scoped secrets.

#### migrate env all

```sh
gh secret-kit migrate env all [flags]
```

Execute all migration steps in sequence: init, create, run, check, and delete.

This command initializes the stub workflow, generates and pushes the migration workflow, triggers it, waits for completion, verifies the results, and cleans up.

**Options:**

- `--branch string`: Branch to push the workflow to (default: "gh-secret-kit-migrate")
- `--dst string` / `-d`: Destination repository (e.g., owner/repo) (required)
- `--dst-env string`: Destination environment name (required)
- `--dst-host string`: GitHub host for the destination (defaults to source repository host)
- `--label string`: Label name for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--overwrite`: Overwrite existing secrets at destination
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--runner-label string`: Runner label for the workflow (default: "gh-secret-kit-migrate")
- `--secrets strings`: Specific secret names to migrate (comma-separated or repeated flag; defaults to all)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--src-env string`: Source environment name (required)
- `--timeout string`: Timeout duration when waiting for workflow completion (e.g., 5m, 1h) (default: "10m")
- `--unarchive`: Temporarily unarchive the repository if it is archived, then re-archive after completion
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

#### migrate env check

```sh
gh secret-kit migrate env check [flags]
```

Compare environment secrets between source and destination repositories. For each secret in the source environment, check whether the corresponding secret (after applying any `--rename` mappings) exists in the destination environment. Exits with a non-zero status if any secrets have not been migrated yet.

**Options:**

- `--dst string` / `-d`: Destination repository (e.g., owner/repo)
- `--dst-env string`: Destination environment name
- `--dst-host string`: GitHub host for the destination (defaults to source repository host)
- `--dst-token string`: PAT or token for the destination (required if destination is on a different host)
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--secrets strings`: Specific secret names to check (comma-separated or repeated flag; defaults to all)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--src-env string`: Source environment name

#### migrate env create

```sh
gh secret-kit migrate env create [flags]
```

Generate a GitHub Actions workflow that migrates environment secrets from the source repository's environment to the destination repository's environment. The workflow is pushed to the source repository on a topic branch.

**Options:**

- `--branch string`: Branch to push the workflow to (default: "gh-secret-kit-migrate")
- `--dst string` / `-d`: Destination repository (e.g., owner/repo)
- `--dst-env string`: Destination environment name
- `--dst-host string`: GitHub host for the destination (defaults to source repository host)
- `--dst-token string`: PAT or token for the destination (required if destination is on a different host)
- `--label string`: Label name for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--overwrite`: Overwrite existing secrets at destination
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--runner-label string`: Runner label for the workflow (default: "self-hosted")
- `--secrets strings`: Specific secret names to migrate (comma-separated or repeated flag; defaults to all)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--src-env string`: Source environment name
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

#### migrate env delete

```sh
gh secret-kit migrate env delete [flags]
```

Close any open pull requests from the migration topic branch and then delete the branch. This removes the generated workflow file and all related resources from the source repository.

**Options:**

- `--branch string`: Branch to delete (default: "gh-secret-kit-migrate")
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--workflow-name string`: Name of the workflow file (default: "gh-secret-kit-migrate")

#### migrate env init

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

#### migrate env run

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

#### migrate list

```sh
gh secret-kit migrate list [org] [flags]
```

List repositories that have at least one repository secret registered.

When called without arguments, the current repository's owner is used as the organization. You can pass an explicit org name (or HOST/ORG) as the first argument. Use `-R`/`--repo` to check a single specific repository instead of scanning an organization.

**Options:**

- `--repo string` / `-R`: Check a single repository (e.g., owner/repo). When specified, org scan is skipped.

#### migrate plan

```sh
gh secret-kit migrate plan [org] [flags]
```

Scan source organization for repositories with secrets, check if matching repositories exist in the destination organization, and output the migration commands for all matching pairs.

This command does not perform any migration; it only outputs the commands that would be needed to migrate secrets from source to destination.

**Arguments:**

- `[org]`: Source organization name (e.g., org or HOST/org). Defaults to current repository owner.

**Options:**

- `--dst string` / `-d`: Destination organization (e.g., org or HOST/org) (required)
- `--runner-label string`: Runner label for the workflow (default: "gh-secret-kit-migrate")

#### migrate org

Migrate organization secrets between organizations.

Each subcommand (`all`, `init`, `create`, `run`, `delete`, `check`) operates on organization-scoped secrets.

#### migrate org all

```sh
gh secret-kit migrate org all [flags]
```

Execute all migration steps in sequence: init, create, run, check, and delete.

This command initializes the stub workflow, generates and pushes the migration workflow, triggers it, waits for completion, verifies the results, and cleans up.

**Options:**

- `--branch string`: Branch to push the workflow to (default: "gh-secret-kit-migrate")
- `--dst string` / `-d`: Destination organization name (required)
- `--dst-host string`: GitHub host for the destination (defaults to source repository host)
- `--label string`: Label name for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--overwrite`: Overwrite existing secrets at destination
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--runner-label string`: Runner label for the workflow (default: "gh-secret-kit-migrate")
- `--secrets strings`: Specific secret names to migrate (comma-separated or repeated flag; defaults to all)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--timeout string`: Timeout duration when waiting for workflow completion (e.g., 5m, 1h) (default: "10m")
- `--unarchive`: Temporarily unarchive the repository if it is archived, then re-archive after completion
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

#### migrate org check

```sh
gh secret-kit migrate org check [flags]
```

Compare organization secrets between source and destination organizations. For each secret in the source, check whether the corresponding secret (after applying any `--rename` mappings) exists in the destination. Exits with a non-zero status if any secrets have not been migrated yet.

**Options:**

- `--dst string` / `-d`: Destination organization name
- `--dst-host string`: GitHub host for the destination (defaults to source host)
- `--dst-token string`: PAT or token for the destination (required if destination is on a different host)
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--secrets strings`: Specific secret names to check (comma-separated or repeated flag; defaults to all)
- `--src string` / `-s`: Source organization name

#### migrate org create

```sh
gh secret-kit migrate org create [flags]
```

Generate a GitHub Actions workflow that migrates organization secrets from the source repository's organization to the destination organization. The workflow is pushed to the source repository on a topic branch.

**Options:**

- `--branch string`: Branch to push the workflow to (default: "gh-secret-kit-migrate")
- `--dst string` / `-d`: Destination organization name
- `--dst-host string`: GitHub host for the destination (defaults to source repository host)
- `--dst-token string`: PAT or token for the destination (required if destination is on a different host)
- `--label string`: Label name for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--overwrite`: Overwrite existing secrets at destination
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--runner-label string`: Runner label for the workflow (default: "self-hosted")
- `--secrets strings`: Specific secret names to migrate (comma-separated or repeated flag; defaults to all)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

#### migrate org delete

```sh
gh secret-kit migrate org delete [flags]
```

Close any open pull requests from the migration topic branch and then delete the branch. This removes the generated workflow file and all related resources from the source repository.

**Options:**

- `--branch string`: Branch to delete (default: "gh-secret-kit-migrate")
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--workflow-name string`: Name of the workflow file (default: "gh-secret-kit-migrate")

#### migrate org init

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

#### migrate org run

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

#### migrate repo

Migrate repository secrets between repositories.

Each subcommand (`all`, `init`, `create`, `run`, `delete`, `check`) operates on repository-scoped secrets.

#### migrate repo all

```sh
gh secret-kit migrate repo all [flags]
```

Execute all migration steps in sequence: init, create, run, check, and delete.

This command initializes the stub workflow, generates and pushes the migration workflow, triggers it, waits for completion, verifies the results, and cleans up.

**Options:**

- `--branch string`: Branch to push the workflow to (default: "gh-secret-kit-migrate")
- `--dst string` / `-d`: Destination repository (e.g., owner/repo) (required)
- `--dst-host string`: GitHub host for the destination (defaults to source repository host)
- `--label string`: Label name for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--overwrite`: Overwrite existing secrets at destination
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--runner-label string`: Runner label for the workflow (default: "gh-secret-kit-migrate")
- `--secrets strings`: Specific secret names to migrate (comma-separated or repeated flag; defaults to all)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--timeout string`: Timeout duration when waiting for workflow completion (e.g., 5m, 1h) (default: "10m")
- `--unarchive`: Temporarily unarchive the repository if it is archived, then re-archive after completion
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

#### migrate repo check

```sh
gh secret-kit migrate repo check [flags]
```

Compare repository secrets between source and destination repositories. For each secret in the source, check whether the corresponding secret (after applying any `--rename` mappings) exists in the destination. Exits with a non-zero status if any secrets have not been migrated yet.

**Options:**

- `--dst string` / `-d`: Destination repository (e.g., owner/repo)
- `--dst-host string`: GitHub host for the destination (defaults to source repository host)
- `--dst-token string`: PAT or token for the destination (required if destination is on a different host)
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--secrets strings`: Specific secret names to check (comma-separated or repeated flag; defaults to all)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)

#### migrate repo create

```sh
gh secret-kit migrate repo create [flags]
```

Generate a GitHub Actions workflow that migrates repository secrets from the source repository to the destination repository. The workflow is pushed to the source repository on a topic branch.

**Options:**

- `--branch string`: Branch to push the workflow to (default: "gh-secret-kit-migrate")
- `--dst string` / `-d`: Destination repository (e.g., owner/repo)
- `--dst-host string`: GitHub host for the destination (defaults to source repository host)
- `--dst-token string`: PAT or token for the destination (required if destination is on a different host)
- `--label string`: Label name for triggering the migration workflow (default: "gh-secret-kit-migrate")
- `--overwrite`: Overwrite existing secrets at destination
- `--rename strings`: Rename mapping in OLD\_NAME=NEW\_NAME format (repeatable)
- `--runner-label string`: Runner label for the workflow (default: "self-hosted")
- `--secrets strings`: Specific secret names to migrate (comma-separated or repeated flag; defaults to all)
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--workflow-name string`: Name of the generated workflow file (default: "gh-secret-kit-migrate")

#### migrate repo delete

```sh
gh secret-kit migrate repo delete [flags]
```

Close any open pull requests from the migration topic branch and then delete the branch. This removes the generated workflow file and all related resources from the source repository.

**Options:**

- `--branch string`: Branch to delete (default: "gh-secret-kit-migrate")
- `--src string` / `-s`: Source repository (e.g., owner/repo; defaults to current repository)
- `--unarchive`: Temporarily unarchive the repository if it is archived
- `--workflow-name string`: Name of the workflow file (default: "gh-secret-kit-migrate")

#### migrate repo init

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

#### migrate repo run

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

#### migrate runner setup

```sh
gh secret-kit migrate runner setup [org] [flags]
```

Register a self-hosted runner and start a message session listener for secret migration. Creates a runner scale set on the source repository/organization, downloads the runner binary, and starts a foreground message session listener. The listener waits for job assignments, automatically starts an ephemeral runner via JIT config when a workflow job is dispatched, and loops continuously until interrupted. Run the workflow dispatch command from another terminal while this command is running.

**Options:**

- `--repo string` / `-R`: Source repository (owner/repo); when omitted uses the first argument as org or falls back to the current repository
- `--runner-label string`: Custom label for the runner (default: "gh-secret-kit-migrate")

#### migrate runner teardown

```sh
gh secret-kit migrate runner teardown [org] [flags]
```

Unregister and stop the self-hosted runner. Stops the runner process, deletes the runner scale set from the source repository/organization, and cleans up local runner files.

**Options:**

- `--repo string` / `-R`: Source repository (owner/repo); when omitted uses the first argument as org or falls back to the current repository
- `--runner-label string`: Label of the runner to tear down (default: "gh-secret-kit-migrate")

### Examples

#### Migrate all repository secrets between repos

```sh
# Terminal 1: Start runner listener (blocks until interrupted)
gh secret-kit migrate runner setup -R owner/source-repo

# Terminal 2: Init, create, run, and clean up
gh secret-kit migrate repo init -s owner/source-repo
gh secret-kit migrate repo create -s owner/source-repo -d owner/dest-repo
gh secret-kit migrate repo run -s owner/source-repo
gh secret-kit migrate repo delete -s owner/source-repo

# After done (Terminal 1), clean up runner
gh secret-kit migrate runner teardown -R owner/source-repo
```

#### Migrate organization secrets

```sh
# Terminal 1: Start runner listener
gh secret-kit migrate runner setup -R org/some-repo

# Terminal 2: Init, create, run, and clean up
gh secret-kit migrate org init -s org/some-repo
gh secret-kit migrate org create -s org/some-repo -d dest-org
gh secret-kit migrate org run -s org/some-repo
gh secret-kit migrate org delete -s org/some-repo

# Clean up runner
gh secret-kit migrate runner teardown -R org/some-repo
```

#### Migrate environment secrets

```sh
gh secret-kit migrate env create \
  -s owner/repo \
  -d owner/repo \
  --src-env staging \
  --dst-env production \
  --secrets API_KEY
```

#### Migrate specific secrets with rename

```sh
gh secret-kit migrate repo create \
  -s owner/source-repo \
  -d owner2/dest-repo \
  --dst-token ghp_xxx \
  --secrets API_KEY,DB_PASSWORD \
  --rename API_KEY=PROD_API_KEY \
  --overwrite

gh secret-kit migrate repo run \
  -s owner/source-repo
```

#### Check migration status

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
