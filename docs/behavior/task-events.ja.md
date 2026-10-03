# Behavior: task events（日本語訳）

[task-events.md](task-events.md) の日本語訳です。正本は英語版で、変更するときは同じ pull request で両方を更新します。

## Task Events

panemux は、panemux のホストとすべての `ssh_connections` のホストで動いているエージェントのセッションを観測し、その状態の変化をすべてイベントとして 1 本の WebSocket で配信する。サーバーは観測して配信するだけで、ある変化を通知するか、枠を点滅させるか、何もしないかは決めない。それは受け手がそれぞれ決める。いまの受け手はブラウザだけで、そのタスクダッシュボード、pane と workspace の attention、ブラウザ通知は、どれも同じストリームからの投影である（「受け手」）。

- **観測するのはタスクの状態だけ。** 入力は、タスクダッシュボードがすでに実行している収集から、停止したセッションの検索を除いたもの（[Collection](tasks.md#collection)）。端末の出力からプロンプトを探すことはしない。エージェントが記録しない状態（codex のコマンド承認待ち、[issue #294](https://github.com/tomo-chan/panemux/issues/294)）は観測されず、エージェントのファイルから分かる状態（`busy`）として表示される。
- **対象はエージェントだけ。** エージェントは、出力に対するパターンではなく、収集に観測を足すことでサポートする。Devin はまだ対象外（[issue #276](https://github.com/tomo-chan/panemux/issues/276)）。エージェントでないプログラムは対象にしない。
- **タスクと pane の対応付けは受け手の役目。** 各イベントはタスクが動いている場所を載せる。サーバーは、どの pane がそれを表示しているか（あるいは表示していないか）を知らない。

### モデル

#### タスク

タスクは動いている 1 つのエージェントセッションで、[`GET /api/tasks`](tasks.md#get-apitasks) のタスク `id` で識別する。panemux のホストでは `local:<agent>:<key>`、接続先では `ssh:<host>:<agent>:<key>`。key は通常セッション ID なので、セッションが動いている間 id は変わらない。変わるのは次の 2 つの場合で、どちらも推測でつなげず、1 つのタスクの削除と別のタスクの追加として配信する。

- まだセッションのない codex のプロセス（`pid-<pid>`）にセッションができたとき
- 動いている Claude Code の中の `/resume` が、プロセスを別のセッションに切り替えたとき（[States](tasks.md#states)）

状態は、[States](tasks.md#states) のうち動いているタスクが取りうる `busy`、`wait`、`idle`、`run`、`unknown`。`stop` は使わない。動かなくなったタスクは **削除（removed）** になる。収集では、終了と `/resume` とホストがプロセスを失ったことを区別できず、どれかを `stop` と呼ぶのは推測になるため。

#### 何を変化とみなすか

観測したタスクはそれぞれ *view*（「タスクの view」の節のフィールド）に落とし、前回の view と `status_since` 以外で違えば変化とする。`status_since` は収集のたびにホストの時計から変換するので、何も変わっていなくても最大 1 秒ほど動く（[States](tasks.md#states)）。そのため変化の時点の値を載せるだけで、比較には使わない。プロセス ID、開始時刻、会話のログは view に含めない。

#### 待ち ID

`wait` のタスクは `wait_id`（そのタスクがいる待ちの識別子）を持つ。受け手は、すでに扱った待ちと新しい待ちを見分けるのに使う。

| タスク | `wait_id` |
|---|---|
| `wait` で、[wait signature](tasks.md#wait-signature) がある | `wait_signature` そのもの（`w1-…`）。収集、再読込、サーバーの再起動をまたいで同じ |
| `wait` で、署名がない（エージェントが待ちの開始を記録していない） | `e1-<epoch>-<seq>`：その待ちを初めて観測した「ストリームの位置」。同じ `waiting_for` のまま待ちが続く間は同じで、サーバーが再起動するまで有効 |
| それ以外の状態 | なし |

2 つめの形は、panemux が待ちを *観測した* 時点を表す。待ちの開始ではなく、`wait_signature` になることもない。サーバーが再起動すると、まだ署名のない待ちには新しい ID が付くので、受け手がその待ちをもう一度扱うことがある。待ちを抜けて再び入ったタスクには、必ず新しい `wait_id` が付く。

#### ホスト

各ホストは状態付きで配信する。

| 状態 | 意味 |
|---|---|
| `pending` | 観測を始めたが、このホストはまだ答えていない |
| `ok` | このホストの最後の収集が成功した |
| `connecting` | 接続をまだ準備している（[Hosts and connections](tasks.md#hosts-and-connections)） |
| `error` | 最後の収集が失敗した。`error` に理由がある |

**`ok` でないホストはタスクのイベントを配信しない。** そのホストのタスクは最後に観測したままで、それが古いことはホストの状態が受け手に伝える。推測で削除することも、待ちのままにしておくこともしない。ホストがまた答えたら、そのタスクを最後に観測したものと比べ、違いを配信する。ホストが失敗している間に始まって終わった待ちは、観測していないので配信されない。1 台のホストの失敗や遅さが、ほかのホストを遅らせたり隠したりすることはない。

`ssh_connections` に足されたホストは `pending` で added として配信する。取り除かれたホストは removed として配信し、そのタスクもそれぞれ removed になる。

読めない・ない・未知の形式のエージェントのファイルから待ちが生まれることはない。収集がすでにそれらを署名なしの `unknown` や `run` として報告しており（[Wait signature](tasks.md#wait-signature)）、それがそのまま配信される。

### `GET /ws/tasks/events`

タスクのイベントストリーム。サーバーは JSON の text フレームを送る。クライアントから送られたものは無視する。

- **認証しない。** [`/ws/{sessionID}`](websocket.md#websocket-protocol) や [`GET /api/tasks`](tasks.md#get-apitasks) と同じく bearer トークンは要らない。
- **cross-site の要求は、upgrade の前、収集を始める前に `403` で拒否する。** `GET /api/tasks` と同じ規則（`Sec-Fetch-Site` が `cross-site` か `same-site`、または `Origin` がサーバー自身でも loopback でもない）に加え、`/ws/{sessionID}` と同じ upgrade 自身の Origin 検査を行う。ストリームを開くとすべてのホストに接続するので、この拒否はその副作用を守る（[Task dashboard collection](../security/command-execution.md#task-dashboard-collection)）。
- クライアントが送るフレームは 512 バイトまで。超えたら接続を閉じる。

#### フレーム

すべてのフレームは `type`、`epoch`、`seq` を持つ。

どの接続でも、最初のフレームはいま分かっていることすべてのスナップショット。

```json
{
  "type": "snapshot",
  "epoch": "9f2c41d07ab35e88",
  "seq": 120,
  "hosts": [
    { "name": "", "status": "ok" },
    { "name": "gpu-box", "status": "error", "error": "connect to gpu-box: dial tcp: i/o timeout" }
  ],
  "tasks": [
    {
      "id": "local:claude:7c21e0a4",
      "host": "",
      "agent": "claude",
      "session_id": "7c21e0a4",
      "cwd": "/workspace/user/panemux",
      "state": "wait",
      "waiting_for": "input needed",
      "wait_id": "w1-6728c5554228dcb7cc58711bbf3636eb24a57f8348e865de9855773e17b9bb47",
      "status_since": "2026-10-02T09:59:58Z",
      "location": { "kind": "tmux", "tmux_session": "task-7c21", "attachable": true }
    }
  ]
}
```

そのあとは、変化ごとに 1 フレームを順に送る。

```json
{ "type": "task", "epoch": "9f2c41d07ab35e88", "seq": 121,
  "op": "changed", "prev_state": "busy",
  "task": { "id": "ssh:gpu-box:codex:0199a6…", "host": "gpu-box", "agent": "codex", "state": "wait", "…": "…" } }

{ "type": "host", "epoch": "9f2c41d07ab35e88", "seq": 122,
  "op": "changed",
  "host": { "name": "gpu-box", "status": "error", "error": "…" } }
```

| `type` | `op` | 載せるもの |
|---|---|---|
| `snapshot` | — | `hosts`、`tasks`（動いているタスク。ないときは `[]`） |
| `task` | `added` | `task`：新しいタスクの view |
| `task` | `changed` | `task`：新しい view。`prev_state`：直前の状態 |
| `task` | `removed` | `task`：最後に観測した view。`prev_state`：その状態 |
| `host` | `added`、`changed`、`removed` | `host` |

状態遷移は、`prev_state` が `task.state` と違う `changed` フレーム。状態が同じ `changed` フレームは、ほかの何か（`wait_id`、`waiting_for`、`cwd`、場所）の変化。最初から待っているタスクが現れたときは、`wait` の `added` フレームになる。

#### タスクの view

| フィールド | 意味 |
|---|---|
| `id`、`host`、`agent`、`session_id`、`cwd` | [`GET /api/tasks`](tasks.md#get-apitasks) と同じ |
| `state` | `busy`、`wait`、`idle`、`run`、`unknown` のどれか |
| `waiting_for` | `wait` のタスクが何を待っているか。エージェントが書いたまま。`wait` のときだけ |
| `wait_id` | 「待ち ID」。`wait` のときだけ |
| `status_since` | タスクがその状態に入った時刻。このフレームの時点でサーバーの時計に変換したもの。比較には使わない（「何を変化とみなすか」） |
| `location` | `kind`、`tmux_session`、`pane_id`、`attachable`。[Where a task runs](tasks.md#where-a-task-runs) と同じ |

会話の本文、プロンプト、コマンドライン、プロセス ID はどのフレームにも載らない。

#### ホストの view

`name`（panemux のホストは `""`）、`status`（「ホスト」）、`error` のときの `error`。

#### 順序：`epoch` と `seq`

`epoch` はサーバーの起動時に決まり、再起動でだけ変わる。`seq` はその epoch で配信したフレームを、すべてのホストとタスクを通して数える。スナップショットの `seq` はそれが含む最後の変化なので、同じ接続の次のフレームは `seq + 1`。

受け手は、`seq` の飛び、`seq` の逆行、違う `epoch`、知らない `type` を見たら、持っているものを捨てて接続し直す。**回復は常に新しいスナップショットから。** サーバーは再送のための履歴を持たない。

### ライフサイクル

```text
 購読者:  0 ──open──▶ 1..n ──最後の close──▶ 0
 観測:    停止 ──▶ 実行 ──購読者がいなくなる──▶ 停止（モデルは保持）
```

- **観測はストリームに購読者がいる間だけ、何人いても 1 本だけ動く。** 最初の購読者が観測を始め、すべてのホストを一度に観測する。まだ答えていないホストは、その購読者のスナップショットで `pending` になる。
- **ホストごとに周期を持つ。** あるホストは、前の観測が終わってから 5 秒後にまた観測する。同じホストの観測は重ならず、遅いホストは（収集のホストごとのタイムアウトまで、[Hosts and connections](tasks.md#hosts-and-connections)）自分の周期を延ばすだけ。ホストの一覧は周期ごとに読み直すので、`ssh_connections` へのホストの追加や削除に追従する。
- **最後の購読者が去ったら観測を止める。** そのため、ページを再読込すると観測は止まって始め直し、すべてのホストをもう一度観測する。最後に観測したもの、epoch、`seq`、署名のない待ち ID は保持するので、再読込でまだ署名のない待ちに新しい ID が付くことはない。観測を再開したら、各ホストは答えるまで `pending` で、保持していたものとの違いを配信する。
- 配信は 1 つの購読者を待たないので、読まなくなった接続がほかの接続を止めることはない。接続のストリームの途中のフレームを落とすことはなく、フレームを受け取れない接続は閉じる。そのタブは接続し直し、新しいスナップショットから続ける。

### 受け手

ブラウザはタブごとに 1 本の接続と 1 つのストアを持ち、タスクの状態が要る画面はすべてそのストアから読む。

#### ストアと接続

- ストアは `epoch`、`seq`、`id` ごとのタスク、名前ごとのホスト、ストリームが `connecting`／`live`／`offline` のどれかを持つ。
- フレームはすべて schema で検証する。検証に通らないフレームは飛びと同じに扱う。
- 閉じたあとは 2 秒から 30 秒のバックオフで接続し直し、**回数の上限は設けない**。サーバーの再起動後も通知が戻るように。ページが hidden の間も接続を保つ。通知はまさにその間のためにある。

#### pane と workspace の attention

- タスクは `location` と `host` から、タスクダッシュボードの Open と同じ規則で pane に対応付ける（[Opening a task](tasks.md#opening-a-task)）。tmux のタスクはそのセッションの `tmux`／`ssh_tmux` の pane に、tmux の外のタスクは `pane_id` が指す `local`／`ssh` の pane に。1 つの tmux セッションの中のタスクはすべて同じ pane になる。codex の daemon のタスクや、どの pane にもない tmux の外のタスクは、どの pane にも対応しない。
- 対応した pane は、タスクが待ちを始めたときに attention を得る。`wait` の `added` フレーム、`wait` への `changed` フレーム、新しい `wait_id` の `changed` フレームのいずれか。スナップショットですでに待っているタスクもその pane に attention を与えるので、再読込でもう一度表示される。ただし、このタブですでに解除した `wait_id` は除く。
- attention の解除は [Agent attention notifications](notifications.md#agent-attention-notifications) のとおり。pane は focus か click で、workspace の tab はその workspace の選択で解除する。加えて、タスクの待ちが終わったとき（`busy`・`idle`・`run` への `changed` フレーム、または `removed`）も、どこで答えたかにかかわらず解除する。ほかにまだ待っているタスクを表示している pane は attention を保つ。`unknown` への変化では解除しない（state file の書き換え中は、1 回の観測だけ `unknown` に読めることがあるため）。ホストが失敗している間も解除しない（タスクの変化が配信されないため）。

#### ブラウザ通知

- ユーザーがいま見ることのできない待ちは、[Agent attention notifications](notifications.md#agent-attention-notifications) の条件で `wait_id` ごとに 1 回通知する。待ちが見えているのは、ブラウザがアクティブで、その pane が画面にあるとき、またはタスクダッシュボードが画面にあってそのタスクを表示しているとき。したがって、どの pane にも対応しないタスクの待ちは、ダッシュボードでだけ見える。
- **タブはそれぞれ自分で判断して通知し、タブ同士で調停しない。** 各タブは自分の画面で見えているかを判断するので、panemux を開いた 2 つのタブが同じ待ちをどちらも通知することがある。
- タブは、通知した `wait_id` をそのタブの session storage に記録する。これはそのタブの再読込では残り、ほかのタブとは共有しないので、再読込や再接続で同じ待ちを再び通知することはない。記録はスナップショットにもうない ID を捨て、新しいものから最大 500 件を保つ。session storage が使えないときは記録をページのメモリに持ち、再読込で再び通知することがある。新しい `wait_id`（同じタスクの後の待ち）はまた通知する。
- 通知の `tag` は `wait_id` なので、同じ待ちの通知がまだ表示されていれば、重ねずに置き換える。
- 通知に出すのはエージェント、ホスト、タスクのディレクトリ名だけ。何を待っているか（`waiting_for`）、会話の本文、プロンプトは出さない。
- クリックするとアプリが前面に出る。対応する pane があれば、その workspace を選択し、それを隠している maximize を解除し、pane を focus して短く outline を出し、attention を解除する。なければタスクダッシュボードを開き、タスクを隠しているフィルターを外して、タスクを選択・強調する。

#### タスクダッシュボード

ダッシュボードは [`GET /api/tasks`](tasks.md#get-apitasks) からタスクを並べる。そこには、ストリームが載せないもの（停止したセッション、git と pull request の詳細、記録、要約）が加わる。タスクの `state`、`waiting_for`、待ちは、ストリームにそのタスクがあればストリームから取るので、変化はダッシュボードの次の収集を待たず、配信されたときに表示される。

## Related Documents

- タスクダッシュボードの収集と状態：[tasks.md](tasks.md)
- 通知の条件と attention の表示：[notifications.md](notifications.md)
- 収集のコマンドの安全性：[Task dashboard collection](../security/command-execution.md#task-dashboard-collection)
- アーキテクチャ：[architecture.md](../architecture.md)
