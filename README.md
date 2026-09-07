# gh-secret-kit

A gh extension for GitHub secrets, variables, environments, and deploy keys.

GitHub does not let you read secret values back, which makes copying, migrating, and auditing them painful.
gh-secret-kit does it for you without ever exposing the values to your machine.

## Why gh-secret-kit?

- **Copy secrets that cannot be read.** Values are moved inside a temporary GitHub Actions workflow, Codespace, or Copilot coding agent session, so they never reach your terminal or shell history.
- **No self-hosted runner required.** `secret copy` runs on a GitHub-hosted runner; a self-hosted runner is only needed with `migrate` when the destination is not reachable from GitHub.
- **Every secret store.** GitHub Actions, Copilot coding agent (Agents), Codespaces, and Dependabot secrets, at repository, organization, and environment scope.
- **Cross-host ready.** Migrate between github.com and GitHub Enterprise Server, and let `migrate plan` generate the full command list for a whole organization.
- **More than secrets.** Variables, environments (settings, deployment branch policies, variables, export/import), and deploy keys move along with them.
- **Know who changed what.** `secret history` reconstructs the create/update/remove history of secrets from the organization audit log.
- **Cleans up after itself.** Temporary branches, tokens, workflows, and run history are removed once an operation finishes.

## Installation

```sh
gh extension install srz-zumix/gh-secret-kit
```

## Shell Completion

gh CLI does not natively support completion for extensions, but a patch script enables it for gh-secret-kit.
Configure [gh completion](https://cli.github.com/manual/gh_completion) for your shell first, then follow the [Shell Completion Guide](https://github.com/srz-zumix/go-gh-extension/blob/main/docs/shell-completion.md).

## Quick Start

```sh
# Copy every repository secret to another repository (no self-hosted runner needed)
gh secret-kit secret copy -R owner/source-repo owner/dest-repo

# Copy organization variables and an environment along with them
gh secret-kit variable copy --owner source-org dest-org
gh secret-kit env copy -R owner/source-repo --src-env staging owner/dest-repo

# Migrate secrets to a host GitHub-hosted runners cannot reach, via a self-hosted runner
gh secret-kit migrate runner setup -R owner/source-repo   # Terminal 1: runner listener
gh secret-kit migrate repo all -s owner/source-repo -d ghes.example.com/owner/dest-repo --dst-token-secret DST_PAT

# Review who changed which secret
gh secret-kit secret history --owner my-org --scope org
```

More recipes are in [Examples](docs/examples.md).

## Commands

| Command | What it does | Reference |
| --- | --- | --- |
| `gh secret-kit secret` | Copy Actions, Agents, and Codespaces secrets to other repositories or organizations, and show secret change history | [secret](docs/commands/secret.md) |
| `gh secret-kit variable` | Copy GitHub Actions variables between repositories and organizations | [variable](docs/commands/variable.md) |
| `gh secret-kit deploy-key` | Add, delete, list, and migrate repository deploy keys, and manage the organization deploy key setting | [deploy-key](docs/commands/deploy-key.md) |
| `gh secret-kit env` | Copy, export, and import GitHub Actions environments and their variables | [env](docs/commands/env.md) |
| `gh secret-kit migrate` | Migrate secrets between repositories, organizations, and environments using a self-hosted runner | [migrate](docs/commands/migrate.md) |

Run `gh secret-kit <command> --help` for the same information from the command line.

## Agent Skills

gh-secret-kit bundles agent skills for AI. Use the `skills` subcommand to install and manage them.

```sh
gh secret-kit skills [subcommand] [args...]
```

For details, see [Songmu/skillsmith](https://github.com/Songmu/skillsmith).

## Documentation

- [Command Reference](docs/commands/README.md) — usage, arguments, and options for every command
- [Examples](docs/examples.md) — copy-and-paste command sequences
- [Migration Guide](docs/migrate.md) ([日本語](docs/migrate.ja.md)) — step-by-step secret migration walkthrough
