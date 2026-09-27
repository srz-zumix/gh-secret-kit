# Copy GitHub Actions Variables

Copy GitHub Actions variables from a source repository or organization to one or more destinations.

```sh
gh secret-kit variable [command]
```

Since variable values are accessible via the GitHub API (unlike secrets), this command reads values directly from the source and writes them to the destination.

The source scope (repository or organization) is controlled by the `--repo` and `--owner` flags: use `--repo` for repository variables (default: current repository when neither is set) or `--owner` for organization variables. The destination scope is inferred for each destination argument: use `owner/repo` for repository scope or `owner` for organization scope.

## variable copy

```sh
gh secret-kit variable copy <dst> [dst...] [flags]
```

Copy all (or specific) GitHub Actions variables from a source repository or organization to one or more destinations. For each variable, if it already exists at the destination and `--overwrite` is not set, a warning is logged and it is skipped; use `--error-if-exists` to treat this as an error instead.

Each destination argument can be `owner/repo` (repository scope) or `owner` (organization scope). Use `--dst-host` to apply a host to destination arguments that do not include one.

With org-to-org copy, repository access is copied by default: each variable's visibility (`all`/`private`/`selected`) and, for `selected`, the granted repositories are reproduced at the destination organization. A `selected` variable whose granted repositories cannot be determined at the source, or none of which exist at the destination, is skipped rather than copied with unverified access. Use `--no-copy-repository-access` to skip copying access.

**Arguments:**

- `<dst> [dst...]`: One or more destination repositories or organizations (required)

**Options:**

- `--copy-repository-access`: With org-to-org variable copy, also copy each variable's visibility and selected repositories to the destination (default: true)
- `--dst-host string`: Host to apply to destination arguments that do not specify one (e.g., `github.com`)
- `--error-if-exists`: Return an error if a variable already exists at destination instead of skipping (default: false)
- `--no-copy-repository-access`: With org-to-org variable copy, skip copying each variable's visibility and selected repositories to the destination (default: false)
- `--owner string`: Source organization/owner for organization-level variables. Mutually exclusive with `--repo`
- `--overwrite`: Overwrite existing variables at destination (default: false)
- `--repo string` / `-R`: Source repository (e.g., `owner/repo`; defaults to current repository). Mutually exclusive with `--owner`
- `--variables strings`: Specific variable names to copy (comma-separated or repeated flag; defaults to all)
