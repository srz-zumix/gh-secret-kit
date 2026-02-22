# instructions

指示が曖昧な場合は編集せずに曖昧な箇所を指摘してください。

ソースコード中のコメントは英語で記載

## README.md

* 各コマンドの Usage を記載します
* markdown の書式警告は修正してください
* completion のヘルプは書かない
* サブコマンドごとにグルーピングして記載します
* サブコマンドを持っているサブコマンドの説明は不要です
* コマンドヘルプの項目は項目のタイトルでソートして記述します
* README に記載するコマンドの説明は下記コメントブロックの内容のように記述してください
* README は英語で記載します

<!--
### コマンドの機能

```sh
コマンド列
```

コマンドの Long の説明
-->

* コマンドの Usage 記載時は、引数やオプションの省略可否・デフォルト値も明記してください
* Usage例・説明文は日本語・英語の混在を避け、統一した言語で記載してください
* Usage例のコマンド列は実際に動作する形式で記載してください
* コマンドの説明文（Long）は簡潔かつ具体的に、何ができるか・どんなオプションがあるかを明記してください
* サブコマンドのグルーピング順は、README全体で一貫性を持たせてください
* READMEのコマンド説明は、実装と乖離しないよう定期的に見直してください
* コマンドの追加・削除・引数変更時は必ずREADMEも更新してください

## コーディング規約

* fmt.Errorf: error strings should not end with punctuation or newlines (ST1005) go-staticcheck
* gh extension 用開発の共通パッケージは github.com/srz-zumix/go-gh-extension/pkg/<path/to/dir> で import

### ソースコード全般

* ディレクトリ・ファイル構成は以下の責務分割に従うこと
  * cmd/: CLIコマンド定義・引数/フラグ処理・cobra.Command生成のみを担当し、ビジネスロジックは持たない
    * cmd/直下に主要コマンド（create.go, delete.go, diff.go, get.go, list.go, member.go, move.go, org.go, rename.go, repo.go, root.go, tree.go, update.go, user.go など）を配置
    * cmd/member/, cmd/org/, cmd/repo/, cmd/user/ などのサブディレクトリに、各コマンドのサブコマンドを配置
    * サブディレクトリ内にもさらに role/, sets/, sync/ などの細分化されたコマンドを配置する場合がある
  * go-gh-extension/pkg: 共通パッケージ群
    * actions/: GitHub Actions関連のユーティリティ
    * cmdflags/: コマンドラインフラグの共通処理
    * gh/: GitHub APIラッパー・ビジネスロジック層。API呼び出しはgh/client/配下で行い、gh/直下はラッパー・ユーティリティ関数のみ
      * gh/配下のラッパー関数は必ずctx context.Context, g *GitHubClientを先頭引数に取り、repository.Repository型等を利用する
    * gh/client/: go-github等の外部APIクライアント呼び出し専用。APIレスポンスの整形やエラーラップは行わない
    * logger/: ロギング関連
    * parser/: 入力値のパース・バリデーション等
    * render/: 表示用の整形・出力処理（テーブル/JSON/hovercard等）
  * config/: 設定ファイル関連
  * version/: バージョン情報管理
* importはローカルパッケージをgithub.com/srz-zumix/go-gh-extension/pkg/<path>で記述し、cmd/から直接github/client/やgo-githubをimportしない
* コマンド追加時はcmd/配下にcobra.Commandを返すNew<Cmd名>Cmd関数を新設し、親コマンドで登録する
* コメントは英語で記載し、関数・構造体・パッケージの責務が明確になるよう記述する
* エラーはcmd/では操作内容を含めてラップし、gh/client/ではラップせずそのまま返す
* テストコードは*_test.goで実装し、各責務ごとに配置する
* コード重複は避け、共通処理は関数化・ユーティリティ化する
* Lint/Formatter（go fmt, go vet, staticcheck等）を通してからコミットする
* 依存関係の循環(import cycle)が発生しないよう注意する

### パッケージ詳細

#### cmd

* 新しいコマンドを作成する場合は他のコマンドの実装を参照し、書き方など踏襲してください
* オプションは基本的に変数で受け取ります
* RunE で処理を実装します
* Args で引数の検証をします
* gh/*.go のラッパー関数を呼び出し、cmd package では github package を import しなくても良い設計にします
* エラーの場合はどういう操作をしようとしてエラーになったかメッセージに含めるようにしてください
* cmd/root.go は変更してはいけません
* cmd/**/*.go のサブコマンドは cobra.Command を return する関数を定義し、その中でコマンド実装してください
  * コマンドの登録は親コマンドの .go ファイルで行います
  * 関数名は New<コマンド名>Cmd としてください。例えば list コマンドであれば NewListCmd 、add コマンドであれば NewAddCmd となります
* owner もしくは repo オプションはオプショナルです。文字列が空の場合の処理は不要です
* サブディレクトリ構成例:
  * cmd/member/add.go, cmd/member/list.go, cmd/member/role.go など
  * cmd/org/role/list.go などの多階層構成も可

#### go-gh-extension/pkg/gh

* gh/client/*.go では API 呼び出しのエラーはフォーマットせずそのまま返します
* gh/*.go には github/client/*.go の関数のラッパーを記述します
  * owner/repo などの string は使わず repository.Repository 型を引数に取ります
  * ラッパー関数は ctx context.Context を第一、 g *GitHubClient を第二引数に取ります
