# GitHub Actions シークレットの移行

このガイドでは、`gh secret-kit migrate` を使ってリポジトリ・組織・環境間で GitHub Actions シークレットを移行する手順を説明します。

## 仕組み

GitHub API はシークレットの値を返さないため、`gh secret-kit migrate` はセルフホストランナーを利用してシークレットを実行時に読み取り、API 経由でコピー先に書き込みます。

全体の流れは次のとおりです：

```text
[ターミナル 1]  migrate runner setup    → コピー元にエフェメラルランナーを登録
[ターミナル 2]  migrate {scope} init    → スタブワークフローをプッシュ、ドラフト PR を作成
               migrate {scope} create  → 移行ワークフローを生成してプッシュ
               migrate {scope} run     → ラベルでワークフローをトリガー
               migrate {scope} check   → 移行結果を検証
               migrate {scope} delete  → ブランチと PR を削除
[ターミナル 1]  migrate runner teardown → ランナーを停止・登録解除
```

`scope` は `repo`、`org`、`env` のいずれかです。

> **注意**: Dependabot シークレットはサポートされていません。Dependabot シークレットは Dependabot によってトリガーされたワークフローからしかアクセスできません。

## 前提条件

- `gh` CLI でコピー元リポジトリに対して十分な権限があること
- スケールセットランナーの実行環境では `gh auth login` 済みでコピー先への権限もある前提です。同一ホスト内の移行であれば追加の設定は不要です
- コピー元リポジトリで GitHub Actions ワークフローを実行できること

## クイックスタート：`all` を使う

各スコープに `all` サブコマンドがあり、すべてのステップ（init → create → run → check → delete）を 1 回で実行できます。ランナーリスナーを先に起動する必要があります。

### リポジトリシークレットを移行する（簡易版）

```sh
# ターミナル 1：ランナーリスナーを起動（Ctrl+C で停止するまでブロック）
gh secret-kit migrate runner setup -R owner/source-repo

# ターミナル 2：全ステップを一括実行
gh secret-kit migrate repo all \
  -s owner/source-repo \
  -d owner/dest-repo

# ターミナル 2 が終わったらターミナル 1 を Ctrl+C で停止してクリーンアップ
gh secret-kit migrate runner teardown -R owner/source-repo
```

### 組織シークレットを移行する（簡易版）

```sh
# ターミナル 1
gh secret-kit migrate runner setup -R org/some-repo

# ターミナル 2
gh secret-kit migrate org all \
  -s org/some-repo \
  -d dest-org

# クリーンアップ
gh secret-kit migrate runner teardown -R org/some-repo
```

### 環境シークレットを移行する（簡易版）

```sh
# ターミナル 1
gh secret-kit migrate runner setup -R owner/repo

# ターミナル 2
gh secret-kit migrate env all \
  -s owner/repo \
  -d owner/dest-repo \
  --src-env staging \
  --dst-env production

# クリーンアップ
gh secret-kit migrate runner teardown -R owner/repo
```

## ステップごとの手順

生成されたワークフローを確認したい場合や、同一ランナーで複数スコープを実行したい場合、特定のステップだけ再実行したい場合は、個別のサブコマンドを使います。

### ステップ 1：ランナーを起動する

**ターミナル 1** で実行します。Ctrl+C で停止するまでブロックします。

```sh
# リポジトリスコープのランナー
gh secret-kit migrate runner setup -R owner/source-repo

# 組織スコープのランナー
gh secret-kit migrate runner setup owner-org
```

| オプション | 説明 |
| --- | --- |
| `--repo` / `-R` | コピー元リポジトリ（`owner/repo`）。省略すると組織スコープになります |
| `--runner-label` | ランナーのカスタムラベル（デフォルト: `gh-secret-kit-migrate`） |
| `--max-runners` | 最大同時実行ランナー数（デフォルト: `2`） |

### ステップ 2：Init

トピックブランチを作成し、スタブワークフローをプッシュして、コピー元リポジトリにドラフト PR を開きます。これにより GitHub がワークフローファイルを認識します。

```sh
gh secret-kit migrate repo init -s owner/source-repo
# または
gh secret-kit migrate org init -s org/some-repo
# または
gh secret-kit migrate env init -s owner/source-repo
```

| オプション | 説明 |
| --- | --- |
| `--src` / `-s` | コピー元リポジトリ（デフォルト: カレントリポジトリ） |
| `--branch` | トピックブランチ名（デフォルト: `gh-secret-kit-migrate`） |
| `--label` | トリガーラベル名（デフォルト: `gh-secret-kit-migrate`） |
| `--workflow-name` | ワークフローファイル名（デフォルト: `gh-secret-kit-migrate`） |
| `--unarchive` | アーカイブ済みリポジトリを一時的にアーカイブ解除する |

### ステップ 3：Create

移行ワークフロー YAML を生成してトピックブランチにプッシュします。

```sh
# リポジトリシークレット
gh secret-kit migrate repo create \
  -s owner/source-repo \
  -d owner/dest-repo

# 組織シークレット
gh secret-kit migrate org create \
  -s org/some-repo \
  -d dest-org

# 環境シークレット
gh secret-kit migrate env create \
  -s owner/source-repo \
  -d owner/dest-repo \
  --src-env staging \
  --dst-env production
```

| オプション | 説明 |
| --- | --- |
| `--src` / `-s` | コピー元リポジトリ（デフォルト: カレントリポジトリ） |
| `--dst` / `-d` | コピー先リポジトリまたは組織（必須） |
| `--secrets` | 移行するシークレット名（カンマ区切り；デフォルト: すべて） |
| `--rename` | リネームマッピング `OLD=NEW`（繰り返し指定可） |
| `--overwrite` | コピー先に同名のシークレットがあれば上書きする |
| `--runner-label` | ワークフローの `runs-on` に使うランナーラベル（デフォルト: `gh-secret-kit-migrate`） |
| `--src-env` | コピー元の環境名（env スコープのみ、必須） |
| `--dst-env` | コピー先の環境名（env スコープのみ、必須） |

### ステップ 4：Run

ドラフト PR のトリガーラベルをトグルして移行ワークフローを起動します。

```sh
gh secret-kit migrate repo run -s owner/source-repo
# または
gh secret-kit migrate org run -s org/some-repo
# または
gh secret-kit migrate env run -s owner/source-repo
```

| オプション | 説明 |
| --- | --- |
| `--src` / `-s` | コピー元リポジトリ（デフォルト: カレントリポジトリ） |
| `--wait` / `-w` | ワークフローの完了を待つ |
| `--timeout` | 待機タイムアウト（例: `5m`, `1h`；デフォルト: `10m`） |

### ステップ 5：Check

コピー元とコピー先のシークレットを比較します。未移行のシークレットがある場合は非ゼロ終了します。

```sh
# リポジトリシークレット
gh secret-kit migrate repo check \
  -s owner/source-repo \
  -d owner/dest-repo

# 組織シークレット
gh secret-kit migrate org check \
  -s source-org \
  -d dest-org

# 環境シークレット
gh secret-kit migrate env check \
  -s owner/source-repo \
  -d owner/dest-repo \
  --src-env staging \
  --dst-env production
```

| オプション | 説明 |
| --- | --- |
| `--src` / `-s` | コピー元リポジトリまたは組織 |
| `--dst` / `-d` | コピー先リポジトリまたは組織 |
| `--secrets` | 確認するシークレット名（デフォルト: すべて） |
| `--rename` | 比較時に適用するリネームマッピング |

### ステップ 6：Delete

ドラフト PR を閉じてトピックブランチを削除します。

```sh
gh secret-kit migrate repo delete -s owner/source-repo
# または
gh secret-kit migrate org delete -s org/some-repo
# または
gh secret-kit migrate env delete -s owner/source-repo
```

### ステップ 7：ランナーのクリーンアップ

ランナーリスナーを停止した後、ランナーを登録解除します。

```sh
gh secret-kit migrate runner teardown -R owner/source-repo
# または組織スコープの場合
gh secret-kit migrate runner teardown owner-org
```

### 残留ランナーを削除する

teardown を実行せずにセットアップが中断された場合、孤立したランナーが GitHub に残ることがあります。`runner prune` で削除できます：

```sh
# プレビュー（削除しない）
gh secret-kit migrate runner prune --dry-run owner-org

# デフォルトラベルの gh-secret-kit- ランナーをすべて削除
gh secret-kit migrate runner prune owner-org

# ラベルに関係なく gh-secret-kit- ランナーをすべて削除
gh secret-kit migrate runner prune --runner-label "" owner-org
```

| オプション | 説明 |
| --- | --- |
| `--repo` / `-R` | コピー元リポジトリ（`owner/repo`）。省略すると組織スコープになります |
| `--runner-label` | このラベルを持つランナーのみ削除（デフォルト: `gh-secret-kit-migrate`；`""` = すべての gh-secret-kit ランナー） |
| `--dry-run` / `-n` | 削除せずにプレビューのみ表示 |

## 組織全体の移行状況を確認する

`migrate check` で組織全体をスキャンし、すべてのシークレットが移行済みかを確認できます：

```sh
gh secret-kit migrate check source-org -d dest-org
```

一致するリポジトリペアのリポジトリシークレット・環境シークレット・組織シークレットをすべてチェックし、合否サマリーを表示します。

## 移行計画を立てる

`migrate plan` で実際の移行を行わずにコマンドをプレビューできます：

```sh
gh secret-kit migrate plan source-org -d dest-org
```

両組織に存在するリポジトリのうちシークレットを持つものについて、実行すべき `migrate repo all` コマンドを出力します。

## シークレットを持つリポジトリを一覧表示する

```sh
# 組織をスキャン
gh secret-kit migrate list source-org

# 特定のリポジトリを確認
gh secret-kit migrate list -R owner/repo
```

## よくあるシナリオ

### 特定のシークレットをリネームしながら移行する

```sh
# ターミナル 1
gh secret-kit migrate runner setup -R owner/source-repo

# ターミナル 2
gh secret-kit migrate repo all \
  -s owner/source-repo \
  -d owner/dest-repo \
  --secrets API_KEY,DB_PASSWORD \
  --rename API_KEY=PROD_API_KEY \
  --overwrite

# クリーンアップ
gh secret-kit migrate runner teardown -R owner/source-repo
```

### クロスホスト移行（GitHub.com → GHES）

```sh
# ターミナル 1
gh secret-kit migrate runner setup -R owner/source-repo

# ターミナル 2
gh secret-kit migrate repo all \
  -s owner/source-repo \
  -d ghes.example.com/owner/dest-repo \
  --dst-token DST_PAT

# クリーンアップ
gh secret-kit migrate runner teardown -R owner/source-repo
```

### plan の出力を使って複数リポジトリを移行する

```sh
# 移行コマンドをプレビュー
gh secret-kit migrate plan source-org -d dest-org

# 組織全体で共通のランナーを 1 つ起動
gh secret-kit migrate runner setup source-org

# plan が出力したコマンドを順に実行
gh secret-kit migrate repo all -s source-org/repo-a -d dest-org/repo-a
gh secret-kit migrate repo all -s source-org/repo-b -d dest-org/repo-b
# ...

gh secret-kit migrate runner teardown source-org
```

## セキュリティに関する注意

- シークレットの値はランナー上のディスクに**一切書き込まれません**。
- 移行ワークフローは `secrets` コンテキストでシークレットを読み取り、GitHub API を直接呼び出してコピー先に設定します。
- 生成されたワークフローとトピックブランチは `delete` によりクリーンアップされます。
- `--dst-token` は**通常ほとんど使われません**。スケールセットランナーが `gh auth login` 済みでコピー先の権限を持っている場合（同一ホスト内の移行など）は不要です。ランナーがコピー先ホストに対して認証されていない場合（例: GitHub.com → GHES などクロスホスト移行）にのみ、コピー先用 PAT を保持するシークレット変数名（例: `DST_PAT`）を指定します。トークンの値はワークフロー YAML に埋め込まれず、実行時に `${{ secrets.DST_PAT }}` として読み取られます。
