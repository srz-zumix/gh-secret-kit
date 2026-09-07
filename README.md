# gh-secret-kit

**Copy and migrate GitHub secrets without manually re-entering their values.**

A [GitHub CLI](https://cli.github.com/) extension for secrets, variables,
environments, and deploy keys.

## Why gh-secret-kit?

- **Copy to multiple destinations at once.** Select, exclude, or rename secrets
  instead of setting them up one repository at a time.
- **Move between secret stores.** Copy from Actions, Copilot coding agent, or
  Codespaces secrets into Actions, Agents, Codespaces, or Dependabot secrets.
- **Migrate across GitHub hosts.** Move between GitHub.com and GitHub Enterprise
  Server, with self-hosted runners for destinations GitHub-hosted runners cannot reach.
- **Bring configuration along.** Copy variables and environment settings, migrate
  deploy keys across hosts, and review secret change history through audit logs.

## Installation

Install [GitHub CLI](https://cli.github.com/) and authenticate with `gh auth login`.
You need permission to read the source configuration and write to the destination;
secret-copy commands also require access to the source runtime.

```sh
gh extension install srz-zumix/gh-secret-kit
```

## Shell Completion

Configure [gh completion](https://cli.github.com/manual/gh_completion) first,
then use the extension completion workaround in the
[Shell Completion Guide](https://github.com/srz-zumix/go-gh-extension/blob/main/docs/shell-completion.md).

## Quick Start

### secret copy

```sh
gh secret-kit secret copy -R owner/source-repo owner/dest-repo other-owner/dest-repo
```

Copy all GitHub Actions repository secrets to one or more destinations.
Replace the repository names with your own; at least one destination is required.
`-R` is optional and defaults to the current repository. Existing destination
secrets are skipped unless you add `--overwrite` (default: false).
Use optional `--secrets` to select names (default: all) or `--rename OLD=NEW`
to rename them (default: no renaming).

The copy runs in a temporary workflow in the source repository, not by reading
secret values through the API. It uses your local `gh` authentication for the
destination by default and removes the temporary branch, token secrets, and run
history after completion. The destination must be reachable from the runner.
See [secret copy](docs/commands.md#secret-copy) for scopes, options, and details;
use the [Migration Guide](docs/migrate.md) when a self-hosted runner is needed.

## Documentation

The [Command Reference](docs/commands.md) contains the full usage, arguments,
options, defaults, and examples previously listed here.

| Task | Reference |
| --- | --- |
| Copy secrets or inspect their change history | [Secrets](docs/commands.md#manage-secrets) |
| Copy repository or organization variables | [Variables](docs/commands.md#copy-github-actions-variables) |
| Manage and migrate deploy keys | [Deploy keys](docs/commands.md#manage-repository-deploy-keys) |
| Copy, export, or import environment configuration | [Environments](docs/commands.md#manage-github-actions-environment-resources) |
| Plan, run, and verify secret migrations | [Migration](docs/commands.md#migrate-github-actions-secrets) |

For a migration walkthrough, see the [Migration Guide](docs/migrate.md)
([Japanese](docs/migrate.ja.md)).

## Agent Skills

Bundled [agent skills](skills/) help AI assistants use gh-secret-kit.
Use `gh secret-kit skills --help` to learn how to install and manage them;
see [skillsmith](https://github.com/Songmu/skillsmith) for details.
