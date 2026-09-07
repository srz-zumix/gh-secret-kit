# Manage Repository Deploy Keys

Manage deploy keys for GitHub repositories.

```sh
gh secret-kit deploy-key [command]
```

## deploy-key add

```sh
gh secret-kit deploy-key add [<public-key>] [flags]
```

Add a deploy key to a repository. The public key can be provided as a positional argument or read from a file via `--key-file`.

**Arguments:**

- `[<public-key>]`: Public key string (optional; use `--key-file` instead if reading from a file)

**Options:**

- `--key-file string` / `-f`: Path to the public key file
- `--read-only`: Create a read-only deploy key (default: false, i.e., read-write)
- `--repo string` / `-R`: Repository to add the deploy key to (e.g., `owner/repo`; defaults to current repository)
- `--title string` / `-t`: Title (label) for the deploy key

## deploy-key delete

```sh
gh secret-kit deploy-key delete <key-id> [flags]
```

Delete a deploy key from a repository by its ID.

**Arguments:**

- `<key-id>`: Deploy key ID (required)

**Options:**

- `--repo string` / `-R`: Repository to delete the deploy key from (e.g., `owner/repo`; defaults to current repository)

## deploy-key get

```sh
gh secret-kit deploy-key get <key-id> [flags]
```

Show detailed information about a specific deploy key by its ID.

**Arguments:**

- `<key-id>`: Deploy key ID (required)

**Options:**

- `--repo string` / `-R`: Repository to get the deploy key from (e.g., `owner/repo`; defaults to current repository)

## deploy-key list

```sh
gh secret-kit deploy-key list [flags]
```

List all deploy keys configured for a repository.

**Options:**

- `--repo string` / `-R`: Repository to list deploy keys for (e.g., `owner/repo`; defaults to current repository)

## deploy-key migrate

```sh
gh secret-kit deploy-key migrate <dst> [flags]
```

Migrate all deploy keys from a source repository to a destination repository.

Note: GitHub does not allow the same public key to be registered as a deploy key in multiple repositories on the same host. This command is intended for migrating keys across different hosts (e.g., `github.com` to a GitHub Enterprise Server instance).

**Arguments:**

- `<dst>`: Destination repository in `[HOST/]OWNER/REPO` format (required)

**Options:**

- `--exclude strings`: Exclude deploy keys whose title contains the specified string (comma-separated or repeated flag; default: none)
- `--repo string` / `-R`: Source repository (e.g., `owner/repo`; defaults to current repository)

## deploy-key setting

```sh
gh secret-kit deploy-key setting [org] [flags]
```

Get or set whether deploy keys are enabled for repositories in an organization. When `--set` is omitted, the current value of `deploy_keys_enabled_for_repositories` is printed. Passing `--set enable` or `--set disable` updates the setting.

Note: Deploy keys must be enabled at the organization level before they can be added to individual repositories.

**Arguments:**

- `[org]`: Organization name (e.g., `myorg` or `HOST/myorg`; defaults to current repository owner)

**Options:**

- `--owner string`: Organization (e.g., `owner` or `HOST/owner`; defaults to current repository owner)
- `--set string`: Set deploy keys setting: `enable` or `disable` (omit to get current value)
