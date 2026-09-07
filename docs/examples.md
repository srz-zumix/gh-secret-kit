# Examples

## Copy repository secrets without a self-hosted runner

```sh
# Copy every repository secret to two destinations
gh secret-kit secret copy -R owner/source-repo owner/dest-repo other-owner/dest-repo

# Copy specific environment secrets with rename
gh secret-kit secret copy \
  -R owner/source-repo \
  --scope env \
  --src-env staging \
  --dst-env production \
  --secrets API_KEY \
  --rename API_KEY=PROD_API_KEY \
  --overwrite \
  owner/dest-repo
```

## Review secret change history

```sh
# Show the last 100 secret events of the current repository and its organization
gh secret-kit secret history

# Show who changed a specific organization secret in 2024
gh secret-kit secret history --owner my-org --scope org --secret API_KEY --since 2024-01-01 --until 2024-12-31

# Show Dependabot secret events of a repository as JSON
gh secret-kit secret history -R owner/repo --secret-type dependabot --format json
```

## Migrate all repository secrets between repos

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

## Migrate organization secrets

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

## Migrate environment secrets

```sh
gh secret-kit migrate env create \
  -s owner/repo \
  -d owner/repo \
  --src-env staging \
  --dst-env production \
  --secrets API_KEY
```

## Migrate specific secrets with rename

```sh
gh secret-kit migrate repo create \
  -s owner/source-repo \
  -d owner2/dest-repo \
  --dst-token-secret DST_PAT \
  --secrets API_KEY,DB_PASSWORD \
  --rename API_KEY=PROD_API_KEY \
  --overwrite

gh secret-kit migrate repo run \
  -s owner/source-repo
```

## Check migration status

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
