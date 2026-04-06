# gh-secret-kit - System Architecture

## Overview

`gh-secret-kit` is a GitHub CLI (`gh`) extension for managing GitHub Actions secrets. It is built in Go using the [cobra](https://github.com/spf13/cobra) CLI framework and communicates with GitHub via the GitHub REST/GraphQL API.

## High-Level Architecture

```mermaid
graph TB
    User["User (Terminal)"]
    CLI["gh-secret-kit (CLI)"]
    GH_API["GitHub API (REST / GraphQL)"]
    SRC_REPO["Source Repository / Org"]
    DST_REPO["Destination Repository / Org"]
    RUNNER["Self-Hosted Runner (Ephemeral)"]
    WORKFLOW["Generated Workflow (GitHub Actions)"]

    User --> CLI
    CLI -->|"API calls (list, check, init, create, delete)"| GH_API
    CLI -->|"runner setup / teardown"| GH_API
    GH_API --> SRC_REPO
    GH_API --> DST_REPO
    CLI -->|"Push workflow YAML"| SRC_REPO
    CLI -->|"Start listener"| RUNNER
    SRC_REPO -->|"Dispatch job"| RUNNER
    RUNNER -->|"Execute"| WORKFLOW
    WORKFLOW -->|"Read secrets via context"| SRC_REPO
    WORKFLOW -->|"Set secrets via API"| DST_REPO
```

## Migration Data Flow

```mermaid
sequenceDiagram
    participant User as User (Terminal 1 & 2)
    participant CLI as gh-secret-kit
    participant API as GitHub API
    participant SrcRepo as Source Repo
    participant Runner as Self-Hosted Runner
    participant Workflow as Migration Workflow
    participant DstRepo as Destination Repo/Org

    Note over User,DstRepo: Terminal 1: Runner Listener
    User->>CLI: migrate runner setup
    CLI->>API: Create runner scale set
    API->>SrcRepo: Register runner
    CLI->>CLI: Download runner binary
    CLI->>Runner: Start message session listener

    Note over User,DstRepo: Terminal 2: Migration Commands
    User->>CLI: migrate scope init
    CLI->>API: Push stub workflow to topic branch
    CLI->>API: Open draft PR
    CLI->>API: Create trigger label

    User->>CLI: migrate scope create
    CLI->>API: Fetch secret names from source
    CLI->>CLI: Generate workflow YAML
    CLI->>API: Push workflow to topic branch

    User->>CLI: migrate scope run
    CLI->>API: Remove & re-add label on PR
    API->>SrcRepo: Trigger pull_request event
    SrcRepo->>Runner: Dispatch workflow job
    Runner->>Workflow: Execute via JIT config

    loop For each secret
        Workflow->>SrcRepo: Read via secrets context
        Workflow->>API: Set secret on destination
        API->>DstRepo: Store secret
    end

    User->>CLI: migrate scope check
    CLI->>API: List source secrets
    CLI->>API: List destination secrets
    CLI->>User: Report comparison result

    User->>CLI: migrate scope delete
    CLI->>API: Close PR & delete branch

    Note over User,DstRepo: Terminal 1: Cleanup
    User->>CLI: migrate runner teardown
    CLI->>API: Delete runner scale set
    CLI->>Runner: Stop & clean up
```

## Module Structure

```mermaid
graph TB
    subgraph main["gh-secret-kit (main module)"]
        MAIN["main.go"]
        VERSION["version/"]
    end

    subgraph cmd["cmd/ (CLI layer)"]
        ROOT["cmd/root.go"]
        MIGRATE_GO["cmd/migrate.go"]
        COMPLETION["cmd/completion.go"]
        MIG_LIST["cmd/migrate/list.go"]
        MIG_ORG_GO["cmd/migrate/org.go"]
        MIG_REPO_GO["cmd/migrate/repo.go"]
        MIG_ENV_GO["cmd/migrate/env.go"]
        MIG_RUNNER_GO["cmd/migrate/runner.go"]
        MIG_TYPES["cmd/migrate/types/options.go"]
    end

    subgraph scope["cmd/migrate/org, repo, env"]
        ORG_CREATE["org/create.go"]
        ORG_CHECK["org/check.go"]
        REPO_CREATE["repo/create.go"]
        REPO_CHECK["repo/check.go"]
        ENV_CREATE["env/create.go"]
        ENV_CHECK["env/check.go"]
    end

    subgraph runner_cmd["cmd/migrate/runner/"]
        RUNNER_SETUP["setup.go"]
        RUNNER_TEARDOWN["teardown.go"]
    end

    subgraph workflow["cmd/migrate/workflow/ (shared)"]
        WF_CONFIG["config.go"]
        WF_INIT["init.go"]
        WF_CREATE["create.go"]
        WF_RUN["run.go"]
        WF_DELETE["delete.go"]
        WF_CHECK["check.go"]
    end

    subgraph pkg["pkg/migrate/ (business logic)"]
        PKG_WORKFLOW["workflow.go"]
        PKG_SCALESET["scaleset.go"]
        PKG_LISTENER["listener.go"]
        PKG_RUNNER_PROC["runner_process.go"]
        PKG_STATE["state.go"]
    end

    subgraph ghpkg["go-gh-extension/pkg/gh/ (API wrappers)"]
        GH_SECRET["secret.go"]
        GH_RUNNER["runner.go"]
        GH_WORKFLOW["workflow.go"]
        GH_REPO["repo.go"]
        GH_ORG["org.go"]
        GH_LABEL["label.go"]
        GH_PR["pr.go"]
        GH_COMMIT["commit.go"]
        CLIENT["client/ (API calls)"]
    end

    subgraph ghutil["go-gh-extension/pkg/ (utilities)"]
        GH_ACTIONS["actions/"]
        GH_CMDFLAGS["cmdflags/"]
        GH_LOGGER["logger/"]
        GH_PARSER["parser/"]
        GH_RENDER["render/"]
        GH_GITUTIL["gitutil/"]
    end

    MAIN --> ROOT
    ROOT --> MIGRATE_GO
    ROOT --> COMPLETION
    MIGRATE_GO --> MIG_ORG_GO
    MIGRATE_GO --> MIG_REPO_GO
    MIGRATE_GO --> MIG_ENV_GO
    MIGRATE_GO --> MIG_LIST
    MIGRATE_GO --> MIG_RUNNER_GO

    MIG_ORG_GO --> ORG_CREATE
    MIG_ORG_GO --> ORG_CHECK
    MIG_ORG_GO --> WF_INIT
    MIG_ORG_GO --> WF_RUN
    MIG_ORG_GO --> WF_DELETE

    MIG_REPO_GO --> REPO_CREATE
    MIG_REPO_GO --> REPO_CHECK
    MIG_REPO_GO --> WF_INIT
    MIG_REPO_GO --> WF_RUN
    MIG_REPO_GO --> WF_DELETE

    MIG_ENV_GO --> ENV_CREATE
    MIG_ENV_GO --> ENV_CHECK
    MIG_ENV_GO --> WF_INIT
    MIG_ENV_GO --> WF_RUN
    MIG_ENV_GO --> WF_DELETE

    MIG_RUNNER_GO --> RUNNER_SETUP
    MIG_RUNNER_GO --> RUNNER_TEARDOWN

    ORG_CREATE --> WF_CREATE
    REPO_CREATE --> WF_CREATE
    ENV_CREATE --> WF_CREATE
    ORG_CHECK --> WF_CHECK
    REPO_CHECK --> WF_CHECK
    ENV_CHECK --> WF_CHECK

    WF_CREATE --> PKG_WORKFLOW
    RUNNER_SETUP --> PKG_SCALESET
    RUNNER_SETUP --> PKG_LISTENER
    PKG_LISTENER --> PKG_RUNNER_PROC

    GH_SECRET --> CLIENT
    GH_RUNNER --> CLIENT
    GH_WORKFLOW --> CLIENT
    GH_REPO --> CLIENT
```

## Package Responsibility

```mermaid
flowchart LR
    subgraph layers["Responsibility Layers"]
        direction TB
        CMD["cmd/<br/>CLI commands<br/>Argument/flag parsing<br/>cobra.Command"]
        PKG["pkg/migrate/<br/>Business logic<br/>Workflow generation<br/>Runner management"]
        GH["go-gh-extension/pkg/gh/<br/>API wrappers<br/>Repository type handling"]
        CLIENT["go-gh-extension/pkg/gh/client/<br/>Raw API calls<br/>No error wrapping"]
    end

    CMD -->|"calls"| PKG
    CMD -->|"calls"| GH
    PKG -->|"calls"| GH
    GH -->|"calls"| CLIENT
    CLIENT -->|"HTTP"| API["GitHub API"]
```

| Layer | Package | Responsibility |
| --- | --- | --- |
| CLI | `cmd/` | cobra.Command definitions, argument/flag parsing, user-facing error messages |
| CLI | `cmd/migrate/` | Parent command registration, scope-level command grouping (org/repo/env), `plan` and `check` commands |
| CLI | `cmd/migrate/workflow/` | Shared init/create/run/delete/check logic across scopes |
| CLI | `cmd/migrate/{org,repo,env}/` | Scope-specific create/check commands |
| CLI | `cmd/migrate/runner/` | Runner setup/teardown commands |
| CLI | `cmd/migrate/types/` | Shared option types |
| Business | `pkg/migrate/` | Workflow YAML generation, scaleset management, runner process lifecycle, listener loop |
| Business | `pkg/migrator/` | Organization-level scan logic: matching repos, collecting env secrets/variables, deploy key discovery |
| Business | `pkg/config/` | Environment configuration export/import: YAML serialization, `Importer` with `ImportOptions` (overwrite, usermap) |
| API Wrapper | `go-gh-extension/pkg/gh/` | GitHub API wrappers using `repository.Repository` type, `ctx` + `*GitHubClient` convention |
| API Client | `go-gh-extension/pkg/gh/client/` | Raw go-github API calls, no error formatting |
| Utility | `go-gh-extension/pkg/{actions,cmdflags,logger,parser,render,gitutil}/` | Cross-cutting utilities |
| Utility | `go-gh-extension/pkg/settings/` | User mapping (login → login): file load, regex-capable `CompiledMappings`, `ResolveSrc` |

## Runner Architecture

```mermaid
graph TB
    subgraph "Local Machine"
        CLI["gh-secret-kit CLI"]
        LISTENER["Message Session Listener"]
        RUNNER_BIN["Runner Binary (downloaded)"]
    end

    subgraph "GitHub"
        SCALESET["Runner Scale Set"]
        REPO["Source Repository"]
        PR["Draft Pull Request"]
        ACTIONS["GitHub Actions"]
    end

    CLI -->|"1. Create scale set"| SCALESET
    CLI -->|"2. Download runner binary"| RUNNER_BIN
    CLI -->|"3. Start listener"| LISTENER
    LISTENER -->|"4. Poll GetMessage"| SCALESET
    CLI -->|"5. Remove/re-add label"| PR
    PR -->|"6. pull_request event"| ACTIONS
    ACTIONS -->|"7. Dispatch job"| SCALESET
    SCALESET -->|"8. Assign job"| LISTENER
    LISTENER -->|"9. Generate JIT config"| RUNNER_BIN
    RUNNER_BIN -->|"10. Execute workflow"| ACTIONS
    RUNNER_BIN -->|"11. Complete"| LISTENER
    LISTENER -->|"12. Loop: wait for next job"| LISTENER
```

## Secret Migration Scopes

```mermaid
graph TB
    subgraph "Source"
        SRC_ORG["Organization Secrets"]
        SRC_REPO["Repository Secrets"]
        SRC_ENV["Environment Secrets"]
    end

    subgraph "Commands"
        ORG_CMD["migrate org create/check"]
        REPO_CMD["migrate repo create/check"]
        ENV_CMD["migrate env create/check"]
    end

    subgraph "Destination"
        DST_ORG["Organization Secrets"]
        DST_REPO["Repository Secrets"]
        DST_ENV["Environment Secrets"]
    end

    SRC_ORG -->|"--src org-name"| ORG_CMD
    ORG_CMD -->|"--dst org-name"| DST_ORG

    SRC_REPO -->|"--src owner/repo"| REPO_CMD
    REPO_CMD -->|"--dst owner/repo"| DST_REPO

    SRC_ENV -->|"--src owner/repo --src-env env"| ENV_CMD
    ENV_CMD -->|"--dst owner/repo --dst-env env"| DST_ENV
```
