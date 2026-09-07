# Manage GitHub Actions Environment Resources

Manage GitHub Actions environment resources such as variables for repository environments.

```sh
gh secret-kit env [command]
```

## env copy

```sh
gh secret-kit env copy <dst> [dst...] --src-env <name> [flags]
```

Copy a GitHub Actions environment (settings, deployment branch policies, and variables) from a source repository to one or more destination repositories.

Each destination argument must be in `owner/repo` format. Use `--dst-host` to apply a host to destination arguments that do not specify one.
The destination environment name defaults to `--src-env` when `--dst-env` is not specified.

Note: Secrets cannot be copied because their values are not accessible via the GitHub API.

**Arguments:**

- `<dst> [dst...]`: One or more destination repositories in `owner/repo` format (required)

**Options:**

- `--dst-env string`: Destination environment name (defaults to `--src-env`)
- `--dst-host string`: Host to apply to destination arguments that do not specify one (e.g., `github.com`)
- `--error-if-exists`: Return an error if a variable already exists at destination instead of skipping (default: false)
- `--overwrite`: Overwrite existing variables at destination (default: false)
- `--repo string` / `-R`: Source repository (e.g., `owner/repo`; defaults to current repository)
- `--src-env string`: Source environment name (required)

## env export

```sh
gh secret-kit env export [flags]
```

Export GitHub Actions environment configurations (settings, deployment branch policies, and variables) to a YAML or JSON file or stdout.
If `--env` is specified, only that environment is exported; otherwise all environments in the repository are exported.

Note: Secrets cannot be exported because their values are not accessible via the GitHub API.

**Options:**

- `--env string`: Environment name to export (defaults to all environments)
- `--format string`: Output format: `{json|yaml}` (default: `yaml`)
- `--output string` / `-o`: Output file path (defaults to stdout)
- `--repo string` / `-R`: Source repository (e.g., `owner/repo`; defaults to current repository)

## env get

```sh
gh secret-kit env get <environment> [flags]
```

Show detailed information about a specific GitHub Actions environment, including settings, wait timer, reviewers, and deployment branch policy.
When the deployment branch policy is `custom`, the configured branch patterns are listed.
Reviewers are shown as `User: <login>` or `Team: <slug>`.

**Arguments:**

- `<environment>`: Environment name (required)

**Options:**

- `--repo string` / `-R`: Repository to get the environment from (e.g., `owner/repo`; defaults to current repository)

## env import

```sh
gh secret-kit env import <input> [flags]
```

Read and apply one or more GitHub Actions environment configurations (settings, deployment branch policies, and variables) from a YAML or JSON file or stdin.
Specify `-` as `<input>` to read from stdin.
Both single-environment and multi-environment (array) formats are supported.
Use `--dryrun` to preview what would be applied without making any changes.
Use `--usermap` to specify a user mapping file that converts reviewer logins during import (as produced by `gh team-kit user map`).

Note: Secrets are not included in the import because their values are not accessible via the GitHub API.

**Arguments:**

- `<input>`: Input file path, or `-` for stdin (required)

**Options:**

- `--dryrun`: Preview changes without applying them (default: false)
- `--env string`: Filter by environment name — only imports environments matching this name from the config file
- `--format string`: Output format: `{json|yaml}` (default: `yaml`)
- `--overwrite`: Overwrite existing environments at destination (default: false; skips environments that already exist)
- `--repo string` / `-R`: Destination repository (e.g., `owner/repo`; defaults to current repository)
- `--usermap string`: User mapping file for reviewer login conversion during import

## env list

```sh
gh secret-kit env list [flags]
```

List all GitHub Actions environments configured for a repository.

**Options:**

- `--repo string` / `-R`: Repository to list environments for (e.g., `owner/repo`; defaults to current repository)

## env variable copy

```sh
gh secret-kit env variable copy <dst> [dst...] [flags]
```

Copy GitHub Actions environment variables from a source repository environment to one or more destination repository environments.

Each destination argument must be in `owner/repo` format. Use `--dst-host` to apply a host to destination arguments that do not specify one.
The destination environment name defaults to `--src-env` when `--dst-env` is not specified.

**Arguments:**

- `<dst> [dst...]`: One or more destination repositories in `owner/repo` format (required)

**Options:**

- `--dst-env string`: Destination environment name (defaults to `--src-env`)
- `--dst-host string`: Host to apply to destination arguments that do not specify one (e.g., `github.com`)
- `--error-if-exists`: Return an error if a variable already exists at destination instead of skipping (default: false)
- `--overwrite`: Overwrite existing variables at destination (default: false)
- `--repo string` / `-R`: Source repository (e.g., `owner/repo`; defaults to current repository)
- `--src-env string`: Source environment name (required)
- `--variables strings`: Specific variable names to copy (comma-separated or repeated flag; defaults to all)
