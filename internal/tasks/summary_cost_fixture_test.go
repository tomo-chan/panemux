package tasks

import (
	"fmt"
	"strings"
)

// A synthetic conversation for measuring what task summaries cost (issue
// #353): no real conversation, path or host is in it. It is written in
// Japanese so that a summary keeping the conversation's language can be
// checked, and it holds what a summary must not get wrong: a correction
// (CSV becomes TSV), work finished along the way, and a last step that
// leaves almost nothing to do.

// costTurn is one line of the synthetic log: a user or assistant message,
// or a tool result, which the excerpt leaves out.
type costTurn struct {
	role string // "user", "assistant" or "tool"
	text string
}

func (c costTurn) line() string {
	switch c.role {
	case "user":
		return userLine(c.text)
	case "assistant":
		return assistantLine(c.text)
	default:
		return toolResultLine(c.text)
	}
}

// costToolOutput is a tool result of about n bytes: the bulk of a real log.
func costToolOutput(label string, n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "%s line %04d: ok\n", label, i)
	}
	return b.String()
}

// costConversation is the conversation as it stands when first summarized.
var costConversation = []costTurn{
	{"user", "サンプルの注文管理アプリ（/workspace/user/project）に、注文一覧を CSV で書き出す機能を追加してください。" +
		"列は注文ID・注文日・顧客名・金額の順です。金額は税込みで、小数点以下は切り捨ててください。" +
		"既存の一覧画面に「書き出し」ボタンを置き、押すとファイルを保存できるようにしたいです。" +
		"テストも書いて、最後に README の使い方の節を更新してください。"},
	{"assistant", "了解しました。まず既存の構成を確認します。注文一覧は `internal/orders` の `List` が返し、" +
		"画面は `web/orders.tsx` が描いているようです。方針は次のとおりです。\n" +
		"1. `internal/orders/export.go` に書き出し処理を追加する（列の順と金額の丸めをここで決める）。\n" +
		"2. `GET /api/orders/export` を追加し、`Content-Disposition` で保存させる。\n" +
		"3. 一覧画面に「書き出し」ボタンを置く。\n4. テストを書く。\n5. README を更新する。"},
	{"tool", costToolOutput("ls internal/orders", 6000)},
	{"tool", costToolOutput("cat internal/orders/list.go", 12000)},
	{"assistant", "`List` は金額を税抜きの整数（円）で持っています。税込みにするには税率 10% を掛けて切り捨てます。" +
		"書き出し処理を `export.go` に追加しました。区切り文字はカンマ、文字コードは UTF-8（BOM なし）、" +
		"改行は CRLF にしています。顧客名にカンマや引用符が含まれる場合は RFC 4180 に従って引用します。"},
	{"tool", costToolOutput("go test ./internal/orders", 8000)},
	{"user", "Excel で開くと文字化けするので BOM を付けてください。それと、金額は税込みではなく税抜きのままで良いです。経理から指摘がありました。"},
	{"assistant", "承知しました。BOM（EF BB BF）を先頭に付け、金額は税抜きのまま出すように変えました。" +
		"税込みの計算は削除しています。テストの期待値も税抜きに直しました。"},
	{"tool", costToolOutput("go test ./internal/orders", 8000)},
	{"assistant", "`GET /api/orders/export` を追加しました。認証済みのユーザーだけが呼べ、" +
		"ファイル名は `orders-YYYYMMDD.csv` です。ハンドラのテストも追加し、通っています。"},
	{"tool", costToolOutput("go test ./internal/api", 10000)},
	{"user", "やっぱり CSV ではなく TSV（タブ区切り）にしてください。顧客名にカンマが多くて、取り込み先のシステムが TSV しか受け付けないそうです。ファイル名の拡張子も .tsv にしてください。"},
	{"assistant", "方針を変更し、TSV で書き出すようにしました。区切り文字をタブに、ファイル名を `orders-YYYYMMDD.tsv` に、" +
		"`Content-Type` を `text/tab-separated-values` に変えています。顧客名にタブや改行が含まれる場合は空白に置き換えます。" +
		"BOM は引き続き付けています。CSV 用の引用処理は不要になったので削除しました。"},
	{"tool", costToolOutput("go test ./...", 15000)},
	{"assistant", "一覧画面に「書き出し」ボタンを追加しました。押すと `/api/orders/export` を開き、ブラウザがファイルを保存します。" +
		"残りは、画面のテストと README の更新です。"},
}

// costToolAppends are the tool results appended while the agent works,
// with no conversation text: a summary of them would say nothing new.
var costToolAppends = [][]costTurn{
	{{"tool", costToolOutput("npm test -- orders", 20000)}},
	{{"tool", costToolOutput("npm run lint", 9000)}},
}

// costTextAppend is new conversation text: the screen test is done and the
// README is what is left.
var costTextAppend = []costTurn{
	{"assistant", "画面のテストを追加しました。ボタンが表示されること、押すと書き出しの URL を開くことを確かめています。テストはすべて通りました。"},
	{"tool", costToolOutput("npm test", 12000)},
	{"user", "ありがとうございます。README の更新をお願いします。終わったらコミットまでしてください。"},
	{"assistant", "README の「使い方」の節に、書き出しボタンと TSV の列（注文ID・注文日・顧客名・金額（税抜き））を追記しました。" +
		"これからコミットします。"},
}

func costLog(turns ...[]costTurn) []byte {
	var lines []string
	for _, group := range turns {
		for _, turn := range group {
			lines = append(lines, turn.line())
		}
	}
	return logWithLines(lines...)
}
