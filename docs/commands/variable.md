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

**Arguments:**

- `<dst> [dst...]`: One or more destination repositories or organizations (required)

**Options:**

- `--dst-host string`: Host to apply to destination arguments that do not specify one (e.g., `github.com`)
- `--error-if-exists`: Return an error if a variable already exists at destination instead of skipping (default: false)
- `--owner string`: Source organization/owner for organization-level variables. Mutually exclusive with `--repo`
- `--overwrite`: Overwrite existing variables at destination (default: false)
- `--repo string` / `-R`: Source repository (e.g., `owner/repo`; defaults to current repository). Mutually exclusive with `--owner`
- `--variables strings`: Specific variable names to copy (comma-separated or repeated flag; defaults to all)
