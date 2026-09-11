# panemux

[English](README.md) | [日本語](README.ja.md)

**ブラウザベースのターミナルマルチプレクサ** — ターミナルを複数のペインに分割し、各ペインからローカルシェル、リモート SSH ホスト、または tmux セッションへ接続できます。すべてのターミナルは xterm.js を使ってブラウザ上に表示されます。

[![CI](https://github.com/tomo-chan/panemux/actions/workflows/ci.yml/badge.svg)](https://github.com/tomo-chan/panemux/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go 1.24](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://golang.org)
[![Releases](https://img.shields.io/github/v/release/tomo-chan/panemux)](https://github.com/tomo-chan/panemux/releases)

---

## 機能

- **4 種類のペイン** — `local`（シェル）、`ssh`（リモート）、`tmux`（ローカルセッションへの接続）、`ssh_tmux`（SSH → tmux）
- **再帰的な分割レイアウト** — 水平・垂直分割を任意の深さまで入れ子にできます
- **ワークスペースタブ** — 複数のレイアウトを定義し、上下左右の任意の辺に配置したタブで切り替えられます
- **ドラッグによるサイズ変更** — ブラウザ上で区切り線をドラッグしてペインのサイズを調整できます
- **ドラッグによる移動** — ペインヘッダーのハンドルをドラッグし、ワークスペースの端、別のペインの端、または区切り線へ移動できます
- **ブラウザ内でのレイアウト編集** — メイン UI からペインの分割、終了、サイズ変更、ターミナルの追加・移動、ワークスペースの管理ができ、変更はすぐに保存されます
- **`~/.ssh/config` 連携** — YAML に設定を重複させることなく、`~/.ssh/config` の任意のホストエイリアスを `connection` として直接参照できます
- **セッションの復元性** — tmux セッションが存在しない場合は自動作成され、終了したペインは画面上の再起動ボタンから再読み込みなしで再接続できます
- **xterm.js による描画** — Unicode とカラー表示に対応した高機能なターミナルエミュレーション
- **単一バイナリ** — Go バックエンドにビルド済みフロントエンドを埋め込むため、別の Web サーバーは不要です
- **YAML 設定** — レイアウト全体と SSH 接続を 1 ファイルで宣言できます。既定のパスは `~/.config/panemux/config.yaml` です
- **Agent Board** — 各ペインのコーディングエージェントの状況を一覧できるダッシュボードと、メッセージ送信用のコマンドパレットを提供します（[セットアップ](#agent-board)）

---

## インストール

### ビルド済みバイナリ

```sh
curl -fsSL https://raw.githubusercontent.com/tomo-chan/panemux/main/install.sh | sh
```

オプションを指定する場合：

```sh
./install.sh --repo tomo-chan/panemux --version v0.2.0 --install-dir ~/.local/bin
```

### ソースからビルド

必要な環境：**Go 1.24 以降**、**Node.js 20 以降**

```sh
git clone https://github.com/tomo-chan/panemux.git
cd panemux
make install-deps   # npm パッケージのインストール + Go モジュールのダウンロード
make build          # bin/panemux を生成
```

---

## クイックスタート

```sh
# 既定の設定で起動（~/.config/panemux/config.yaml があれば読み込み、
# なければローカルシェルを 1 つ起動）
./bin/panemux

# 指定した設定ファイルを読み込む
./bin/panemux --config config.yaml

# ポートを上書きし、Chrome を自動的に開く
./bin/panemux --port 9090 --open
```

ブラウザで [http://localhost:8080](http://localhost:8080) を開きます。

---

## panemux の使い方

### 日常的な使い方

1. `panemux` を起動し、ブラウザで開きます。
2. ワークスペースタブをクリックし、開発、運用、本番環境などのレイアウトを切り替えます。
3. 各ターミナルペインを、通常のシェル、SSH セッション、tmux 接続セッションと同じように操作します。
4. レイアウトを変更したくなったら、メイン UI からペインを並べ替えたり、ターミナルを追加したりします。

### ワークスペースタブ

- 各ワークスペースは、独立したレイアウトツリーとペインの集合を持ちます。
- タブをクリックすると、背後のセッションを再起動せずにワークスペースを切り替えられます。
- 非表示のワークスペースで対応が必要になると、そのワークスペースを開くまでタブが点滅します。
- ワークスペースバーから、ワークスペースの追加、名前の直接変更、削除、タブバーの上下左右への移動ができます。
- バーが左または右にある場合は、内側の端をドラッグして幅を変更できます。この縦向きバーの共通幅はワークスペース設定に保存されます。
- ワークスペースバーでは、ワークスペースごとのペイン状態もひと目で確認できます。各タブにはペイン名の概要が表示され、接続状態、SSH ホストエイリアス、リポジトリ、ブランチ、PR 番号を示す詳細カードも表示できます。
- バーが上または下にある場合、ペイン詳細カードはワークスペースタブに付随するホバー／フォーカスオーバーレイとして表示されるため、ターミナル領域の高さを維持できます。
- バーが左または右にある場合、ペイン詳細カードは各ワークスペースタブの下に常時展開され、余分なポインター操作なしでワークスペースとペインの対応を確認できます。

よくある使い方：

- ローカルの開発用シェルと本番環境の SSH ペインを別々のワークスペースに分ける。
- tmux 専用のワークスペースと、短時間だけ使うローカルシェルを別々のタブに置く。

### ペインの操作

- **ペインのサイズ変更** — 隣接するペイン間の区切り線をドラッグします。
- **ペインの分割** — ヘッダーの操作ボタンから、隣に新しいローカルペインを作成します。
- **ターミナルの追加** — ワークスペースバーから、空のローカルペインを作成するか既存ペインの設定を複製し、挿入位置を選びます。
- **ペインの移動** — ヘッダーのハンドルをドラッグします。ワークスペースの端にドロップすると外側の新しい分割が作成され、別のペインの端や区切り線にドロップするとその位置へ挿入されます。
- **ペインを閉じる** — ヘッダーの操作ボタンから閉じると、レイアウトが自動的に詰められます。
- **ペインの再起動** — 背後のセッションが終了した場合は、画面上のボタンで再起動できます。
- **関連 PR を開く** — 現在の Git ブランチに GitHub Pull Request がある場合、ペインヘッダーから開けます。ローカルペインでは、再開した Codex セッションを含む対話型の `codex` または `claude` セッションが使っている作業ツリーを優先できます。エージェント終了後も、より新しい有効なコンテキストが検出されるまで、最後に有効だった関連作業ツリーを維持します。
- **VS Code を開く** — 対応するペインのヘッダー操作から開けます。ペインの Git／PR 情報と同様に、対話型の `codex` または `claude` セッションが使っている作業ツリーを優先でき、エージェント終了後も最後に有効だった関連作業ツリーを維持します。

### 通知と対応要求の表示

- panemux はターミナル出力を監視し、許可や続行確認など、エージェントからの確認要求を検出します。
- MCP ツールの許可確認など、Codex の権限メニューも検出します。
- 検出すると、ペインの枠が強調表示されます。
- 非アクティブなワークスペースに属するペインの場合、そのワークスペースのタブも強調表示されます。
- 通知権限の付与後、確認要求が現在ユーザーに見えていない場合にだけブラウザ通知が表示されます。通知をクリックすると、該当するワークスペースへ切り替わります。
- panemux はペインごとに最後にブラウザ通知した確認要求を記憶するため、再読み込み、再接続、最大化の切り替えによって同じ要求が再通知されることはありません。
- 通知権限は最初の確認要求を待たず、ブラウザ上で最初に操作したときに求められます。

### ペイン種別の選び方

- panemux サーバーと同じマシン上の通常のシェルには `local` を使います。
- ペインごとに 1 つのリモートシェルを使う場合は `ssh` を使います。
- 永続化されたローカル tmux セッションへ再接続する場合は `tmux` を使います。
- リモートホスト上の永続化された tmux セッションを使う場合は `ssh_tmux` を使います。

### SSH と tmux の使い方

- `connection: my-host` を指定したペインでは、名前付きの `ssh_connections` エントリーまたは `~/.ssh/config` の `Host my-host` エントリーを利用できます。
- `tmux` と `ssh_tmux` のペインは、対象の tmux セッションが存在しない場合に自動作成します。
- `tmux` と `ssh_tmux` のペインでは、通常のドラッグは tmux のマウス操作に従います。ブラウザ側で強制的にテキストを選択するには、macOS では `Option`、Linux と Windows では `Shift` を押しながらドラッグします。
- シェルを特定のディレクトリで起動するには、`local`、`ssh`、`ssh_tmux` のペインに `cwd` を設定します。
- ペイン設定ダイアログでは、ローカルペインと SSH 接続ペインのどちらでも、参照可能なディレクトリツリーから `Working Directory` を選べます。切り替え操作で隠しディレクトリも表示できます。
- ペインヘッダーは、ローカルと SSH 接続の両方について、対話型の `codex`／`claude` 作業ツリーを含む現在の作業コンテキストから Git と PR の情報を解決します。最近有効な関連作業ツリーを検出した場合、ペインが別のリポジトリコンテキストへ移るまで、エージェント終了後もその作業ツリーを維持します。`tmux` と `ssh_tmux` では、現在アクティブな tmux ペインだけを使用します。

---

## Agent Board

Agent Board では、各ペインのコーディングエージェントが何をしているかを 1 つの画面で確認し、同じ場所からメッセージを送れます。次の 2 つは独立しており、どちらか一方だけでも利用できます。

- **ダッシュボード** — 各ペインが自己申告した状態（ステータス、リポジトリ、ブランチ、PR、概要）と、ペイン間のメッセージ履歴を表示します。**Agent Board** ボタンまたは `Cmd/Ctrl+Shift+B` で開きます。
- **コマンドセンター** — 自然言語で「どのペインがブロックされている？」「全ペインにブランチが凍結されたと伝えて」のように依頼できる、Spotlight 風のパレットです（`Cmd/Ctrl+Shift+K`）。

完全な設計は [docs/agent-board.md](docs/agent-board.md) を参照してください。

### 前提条件

ボードに参加するペインの全ホストに、**[agmsg](https://github.com/fujibee/agmsg) があらかじめインストールされている必要があります**。`ssh`／`ssh_tmux` ペインのリモートホストも対象です。panemux は **agmsg 1.2.0** でテストされています。起動時に各ホストの `VERSION` を読み取り、それ以外のバージョンには処理を止めず警告を記録します。agmsg が互換性を保証しているのは `scripts/api.sh` を介した読み取りだけですが、panemux は `send.sh`、`join.sh`、`delivery.sh` にも依存するため、別バージョンでは動作する場合も不具合が起きる場合もあります。panemux が agmsg のインストール、更新、管理を行うことはありません。設定されたパスに `scripts/api.sh` があるかどうかだけを確認します。存在しない場合、ホスト名と確認したパスを含む警告を 1 件記録してそのホストをスキップします。そのホストのペインがボードに表示されないだけで、ほかの動作には影響しません。

コマンドセンターを使うには、panemux を実行するマシンに `claude` CLI も必要です。コマンドセンター自体に agmsg は必要ありません。

リモートホストでは SSH の exec チャネル経由でスクリプトを実行するため、`.bashrc`／`.profile` は読み込まれません。agmsg が必要とするもの（`bash`、`node`、`sqlite3`）は非対話セッションの `PATH` に含める必要があります。対話セッションで `nvm`／`asdf` 配下に agmsg をインストールした場合に見落としやすい点です。

### 設定

```yaml
server:
    host: 127.0.0.1
    # コマンドセンターで必須。空のままにすると初回起動時に panemux が生成し、
    # ~/.config/panemux/token に保存する（このファイルには保存しない）。
    auth_token: ""

command_center:
    enabled: true            # 既定値は false

agent_board:
    team: panemux                      # ボード対応ペインすべてで共有する agmsg チーム
    agmsg_path: ~/.agents/skills/agmsg # ~ はリモートを含む各ホストで展開される

workspaces:
    items:
        - id: default
          title: Default
          layout:
            direction: horizontal
            children:
                - pane:
                    id: api          # この id がペインの agmsg ID になる
                    type: local
                    agent_board:
                        enabled: true
                        mode: monitor  # monitor（既定）| turn | both | off
                  size: 50
```

ペイン ID はボード上のアドレスになるため、`pane-1` ではなく、見分けやすい名前（`api`、`web`、`infra`）を付けてください。`_system` は予約済みであり、設定の検証時に拒否されます。

この設定のために YAML を編集する必要はありません。ペインヘッダーの **Pane Settings** には *Join the agent board* チェックボックスがあり、有効にするとモードを選ぶ *Message delivery* が表示されます。変更はほかのペイン設定と同様に `config.yaml` へ保存されます。

### ペインが参加する仕組み

参加は自動的に行われるため、手動操作は不要です。

1. 通常どおりペイン内でエージェント（`claude`、`codex`、`cursor-agent`、`gemini`、`grok`、`opencode`）を起動します。panemux は 5 秒ごとに確認し、2 回連続で検出する必要があります。
2. panemux は、そのペインのホスト上の `agmsg_path` に agmsg が存在することを確認します。
3. ペイン ID を agmsg のエージェント ID として `join.sh` を実行し、以後 `send.sh` でボードメッセージを送り、定期的に状態を自己申告するよう求める**一度限りの指示をペインのターミナルへ書き込みます**。

この指示と、それに対するエージェントの応答はペイン上に表示されます。これは意図した動作です。ユーザー自身のキー入力と同じ方法でターミナルへ書き込まれるため、見えないところでコマンドの途中に処理が割り込むことはありません。書き込みはペインごとに一度だけで、panemux は完了済みのペインを再起動後も記憶します。

ペイン ID を agmsg のエージェント ID として使うのは意図的なものです。ボード上のすべてのアドレスは `from`／`to` がペイン ID であることを前提としているため、エージェントが独自の名前を選ぶと宛先指定が壊れます。

### 配信モードと必要な初期設定

`agent_board.mode` は、メッセージをペインのエージェントへ届けるかどうかを決めます。既定値は通知を抑えたモードなので、値を選ぶ前に違いを確認してください。

| mode | リポジトリへの書き込み | ブロードキャストがエージェントへ届くか |
|---|---|---|
| `monitor`（既定） | なし | **届かない** — エージェントが確認するまで agmsg に残る |
| `turn`／`both` | あり（1 ファイル） | 届く |

`monitor` では panemux が agmsg の `delivery.sh` を実行しないため、配信フックは作成されず、ボードは実質的に読み取り専用です。ペインは状態を報告し、ユーザーはそれを確認できますが、ブロードキャストが相手へプッシュされることはありません。メッセージ機能を使う場合は `turn` または `both` を選んでください。

`turn` と `both` では、エージェントが agmsg の `delivery.sh` を実行し、panemux ではなく **agmsg** がペインのプロジェクトディレクトリへフックファイルを書き込みます。パスは agmsg がエージェント種別ごとに定めた規約（`scripts/drivers/types/<type>/type.conf` の `hooks_file=`）に従います。agmsg はプロジェクトからの相対パス以外を拒否するため、ユーザー単位の場所へ変更することはできません。

| エージェント種別 | agmsg が書き込むファイル |
|---|---|
| claude-code | `.claude/settings.local.json` |
| codex | `.codex/hooks.json` |
| gemini | `.agent/rules/agmsg.md` |
| opencode | `.opencode/rules/agmsg.md` |
| cursor | `.cursor/rules/agmsg.mdc` |
| grok-build | `.grok/rules/agmsg.md` |

これらはマシン固有のローカルファイルであり、バージョン管理に含めないでください。作業するリポジトリごとに `.gitignore` を編集する代わりに、マシンごとに一度、グローバル除外ファイルを設定します。

```sh
git config --global core.excludesFile ~/.gitignore_global
cat >> ~/.gitignore_global <<'EOF'
.claude/settings.local.json
.codex/hooks.json
.agent/rules/agmsg.md
.opencode/rules/agmsg.md
.cursor/rules/agmsg.mdc
.grok/rules/agmsg.md
EOF
```

`ssh`／`ssh_tmux` ペインを実行する各ホストで、この設定を一度ずつ行ってください。書き込みは何度実行しても同じ結果になります。`delivery.sh set` は agmsg 自身のフックエントリーを削除してから追加し直します。ただし、設定はペインを閉じた後も残り、後から `agent_board.enabled: false` に設定しても panemux は元に戻しません。削除は panemux の外で、agmsg の `delivery.sh set off` を使って行います。

### 動作確認

ダッシュボードを開きます。エージェントが実際に状態を送信してからペインが表示されるため、起動後しばらく待ってください。

何も表示されない場合は、可能性が高い順に次を確認します。

- **panemux が確認した場所に agmsg がない。** 起動ログには、影響を受けるホストごとに `no agmsg installation at "<path>" on host "<host>"` という警告が 1 件記録されます。そのホストに `<agmsg_path>/scripts/api.sh` が存在することを確認してください。
- **エージェントが検出されていない。** ヘッドレス実行（`claude -p`、`codex exec`）は意図的に無視されるため、エージェントをペイン内で対話的に実行する必要があります。
- **エージェントがまだ状態を報告していない。** 状態はすべてエージェントによる自己申告であり、panemux が算出するものではありません。5 分以上前のカードは薄く表示され、`stale` と記されます。
- **リモートの `PATH`。** 前述の前提条件を確認してください。

### コマンドセンターでできること・できないこと

コマンドセンターには、ボード状態の読み取り、メッセージ履歴の読み取り、ペインへのメッセージ送信という 3 つのツールだけがあります。**シェル、ファイルシステムアクセス、ネットワークアクセスはありません**。コードの書き込み、テストの実行、Pull Request の作成はできません。これは単なる指示ではなく、起動方法によって強制されています（[docs/security.md](docs/security.md#command-center-subprocess-execution) を参照）。

送信されたメッセージは、受信側エージェントへの通常のメッセージであり、事前承認済みのコマンドではありません。受信側エージェント自身の確認動作は引き続き適用されるため、「送信済み」は「完了」を意味しません。

---

## 設定

既定の設定パスは `~/.config/panemux/config.yaml` です（初回保存時に自動作成されます）。`config.example.yaml` を出発点としてコピーしてください。

```yaml
server:
  port: 8080
  host: "127.0.0.1"

# 名前付き SSH 接続（任意。~/.ssh/config のホストも直接利用できる）
ssh_connections:
  prod-web:
    host: "192.168.1.10"
    port: 22
    user: "deploy"
    key_file: "~/.ssh/id_ed25519"

# ワークスペースタブ。それぞれが独自の再帰的レイアウトツリーを持つ
workspaces:
  active: dev
  tab_position: top           # top | bottom | left | right
  vertical_bar_width: 280     # tab_position が left/right のときの共通幅（px）
  items:
    - id: dev
      title: "Development"
      layout:
        direction: horizontal # horizontal | vertical
        children:
          - size: 50          # パーセント（同階層の合計は 100）
            pane:
              id: "local-main"
              type: local     # local | ssh | tmux | ssh_tmux
              shell: "/bin/zsh"
              cwd: "~/development"
              title: "Dev Shell"
          - size: 50
            pane:
              id: "ssh-prod"
              type: ssh
              connection: prod-web
              title: "Prod Web"
    - id: ops
      title: "Operations"
      layout:
        direction: horizontal
        children:
          - size: 100
            pane:
              id: "ops-shell"
              type: local
              title: "Ops Shell"
```

トップレベルに `layout:` を持つ古い設定ファイルも引き続き利用できます。次に設定が保存されると、panemux はそのレイアウトを `default` ワークスペースへ移行し、`workspaces:` 形式で書き込みます。

ワークスペースバーから、いつでもワークスペースを操作できます。新しく追加したワークスペースは、ローカルターミナルペインが 1 つある状態で始まり、すぐにアクティブになって設定へ保存されます。同じバーから、ワークスペース名の直接変更、確認付きの削除、`+` による追加、タブ位置の変更もできます。バーが縦向きの場合、その幅は全ワークスペースで共有され、設定へ保存されます。さらに、ワークスペースをまたぐ運用状況の一覧としても機能します。ペイン詳細カードを選択してそのペインへフォーカスしたり、カードを別のワークスペースへドラッグしてペインを移動したりできます。アクティブなワークスペースの切り替え、`tab_position` の変更、縦向きワークスペースバーのサイズ変更は保存されるため、再起動後も同じナビゲーション表示が復元されます。Codex の MCP 許可メニューを含むエージェントの確認要求は、非アクティブなワークスペースタブを強調表示し、通知権限が付与されていればブラウザ通知を表示できます。通知をクリックすると、該当するワークスペースへ切り替わります。

### ペイン種別

| 種別 | 説明 |
|---|---|
| `local` | ローカルシェルプロセス（`shell`、`cwd` は任意） |
| `ssh` | SSH 接続（`ssh_connections` の名前または `~/.ssh/config` のホストエイリアス） |
| `tmux` | ローカル tmux セッション（`tmux_session`）へ接続。存在しない場合は自動作成 |
| `ssh_tmux` | ホストへ SSH 接続後、tmux セッションへ接続。存在しない場合は自動作成 |

### SSH 接続

接続は 2 通りの方法で定義できます。

**YAML 設定の `ssh_connections`** — `host`、`user`、`port`、`key_file`、`password`、`known_hosts_file` を指定できます。

**`~/.ssh/config`** — ワイルドカードを含まないすべての `Host` エントリーが、自動的に `connection` 名として利用可能になります。ファイルから `HostName`、`User`、`Port`、`IdentityFile` が読み込まれます。YAML に重複して記述することなく、既存の SSH 設定を再利用できます。

同じ名前が両方にある場合は、`ssh_connections` が優先されます。

認証は、設定した `key_file` → 設定した `password` → 既定の鍵ファイル（`~/.ssh/id_ed25519`、`~/.ssh/id_rsa`、`~/.ssh/id_ecdsa`）の順に試行されます。

---

## 開発

### 前提条件

- Go 1.24 以降
- Node.js 20 以降

### セットアップ

```sh
make install-deps   # 初回のみ：npm install + go mod download + リポジトリ固有の pre-push フック設定
```

### 開発サーバー

```sh
make dev-backend    # Go バックエンド（:8080）
make dev-frontend   # Vite 開発サーバー（:5173、/api と /ws を :8080 へプロキシ）
```

### 品質ゲート

```sh
make check   # lint + test + coverage（ビルド前に成功している必要がある）
```

`make install-deps` は、追跡対象の `.githooks/pre-push` フックも設定します。このフックは `git push` の前に毎回 `make check` を実行します。

個別のコマンド：

```sh
go test ./... -v -race           # Go テスト
cd frontend && npm test          # フロントエンドテスト
make test-e2e                    # ブラウザ描画のリグレッションに対する Playwright E2E
make coverage-go                 # Go カバレッジ（80% 以上が必須）
cd frontend && npm run coverage  # フロントエンドカバレッジ（80% 以上が必須）
go vet ./...                     # Go の lint
cd frontend && npx tsc --noEmit  # TypeScript の型チェック
```

---

## コントリビューション

1. リポジトリをフォークし、feature ブランチを作成します。
2. テストを先に書いて失敗することを確認してから、変更を実装します。
3. `make check` を実行し、すべてのチェックが成功することを確認します。
4. ローカルの `pre-push` フックが成功してから push します。
5. 変更内容と理由を説明した Pull Request を `main` に対して作成します。

Pull Request には 1 つの論理的な変更だけを含めてください。焦点を絞ることでレビューが速くなり、履歴も明確になります。

---

## ライセンス

[MIT](LICENSE) — Copyright (c) 2026 tomo-chan
