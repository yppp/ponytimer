# NOTES

進捗ログ（ステップごとの記録）。

---

## Step A — Go モジュールの骨格とタイマーコア

### ビルドしたもの

- Go モジュール `github.com/yppp/ponytimer`（`go 1.27.4`、外部依存なし・標準ライブラリのみ）
- `cmd/ponytimer/main.go`
  - 単一バイナリのエントリポイント。フラグ `-addr`（既定 `:8080`）と `-data`（既定 `ponytimer.db`）。
  - `-data` はこのステップでは受け取って保持するのみ（SQLite は後続ステップで接続）。
  - 現状は `/healthz`（JSON `{"status":"ok"}`）と、ルート `/` のプレースホルダのみ。
- `internal/core` — タイマーコア（純粋なドメインロジック、HTTP/DB 非依存）
  - `clock.go`: `Clock` インターフェース、`RealClock`、テスト用の `FakeClock`（`Set` / `Advance`）。
  - `level.go`: `Level`（Index / Type / SB / BB / Ante / DurationSec）、`State` 列挙
    （Waiting / Running / Paused / Break / Finished、spec §5.1 の `waiting|running|paused|break|finished` に対応）。
  - `tournament.go`: `Tournament` 集約とコマンド `Start` / `Pause` / `Resume` / `AdjustTime` /
    `NextLevel` / `PrevLevel` / `Reset`、`Tick(now)`。
  - `snapshot.go`: `Snapshot()`（表示に必要な全項目）と `LevelView`。
- `.github/workflows/ci.yml`: push / pull_request で ubuntu-latest、actions/setup-go@v5
  （`go-version-file: go.mod`）→ `go vet ./...` → `go test ./...`。

### 仕様の要点と実装の選択

- **正本は絶対時刻**（spec §5.1 / D1）: 実行中は `LevelEndsAt`（サーバー絶対時刻）を正本とし、
  `RemainingSec` は paused/waiting のときだけ意味を持つ。
- **Pause の確定時刻**（spec §6.3 / D6）: `Pause(now)` は受信時刻 `now` を使って
  `RemainingSec = LevelEndsAt - now` を確定し、`LevelEndsAt` を nil にする。
  以後 `FakeClock` を進めても `RemainingSec` は変化しない（テストで確認）。
- **ドリフトしない進行**: `Tick(now)` は `now` ではなく **元の `LevelEndsAt` + 次レベルの所要時間** を
  次の終了時刻にする。複数レベルが経過している場合はcascadeし、遅延して Tick しても誤差が蓄積しない。
- **Break 状態**: 内部状態は Running を保持し、`State()` が現在行の `Type` が `break` のとき `Break` を返す。
  Break が終わって通常レベルに戻ると自動的に `Running` になる。
- **Snapshot**: `level_ends_at_ms` は nullable（実行中のみ値を持つ）、`remaining_sec` は
  paused なら凍結値、running なら `LevelEndsAt - now` の生存値。
  `AverageStack = TotalChips / PlayersLeft`（`PlayersLeft == 0` のとき 0）。

### テスト（internal/core/tournament_test.go、FakeClock ベース）

- Start 後の最初のレベル終了時刻
- Tick によるレベル進行（ドリフトなし）＋複数レベルの cascade
- Pause が受信時刻で残り時間を凍結（時計を進めても変化しない）
- Resume が終了時刻を再構築
- AdjustTime: 実行中の終了時刻シフト／paused 中の残り時間シフト（0 でクランプ）
- NextLevel / PrevLevel（先頭より前はエラー）
- Reset が先頭レベル・Waiting へ戻す
- 最終レベル後に Finished
- Break 行で State が `break`、次の通常レベルへ戻ると `running`
- Snapshot の内容（現在／次レベル、終了時刻 ms、各カウント、アベレッジスタック）
- 残り人数 0 のときアベレッジスタックが 0

### ローカル検証結果

```
$ gofmt -l .          # 出力なし
$ go vet ./...        # 問題なし
$ go test ./...
?   	github.com/yppp/ponytimer/cmd/ponytimer	[no test files]
ok  	github.com/yppp/ponytimer/internal/core	0.004s
```

### 残課題 / 次ステップへ

- `TotalChips` の式は本ステップの指示どおり
  `Entries*StartingStack + Rebuys*StartingStack + Addons*AddonChips` とした。
  spec §6.2 は「(エントリー＋リエントリー)×開始スタック ＋ リバイ×リバイスタック ＋ アドオン×アドオンスタック」で、
  リエントリーが総チップ量に含まれる点とリバイスタックが別項目である点が異なる。
  カウント-operation を実装するステップでどちらの定義に寄せるか決めること。
- `-data` の SQLite 接続、`/api/tournaments/...` の REST、SSE 配信、管理 Web と表示画面、
  ストラクチャー編集（§8）と自動生成（§9）はいずれも未実装。
- 永続化（spec §12）: `Tournament` の状態は DB に保存・復元できる必要がある。
  保存形式を決める際、`State()` が派生値である点（Break は行の Type から決まる）に注意。

---

## Step B — SQLite、REST コマンド、Snapshot、SSE

### ビルドしたもの

- `internal/store/store.go`: SQLite スキーマ、トーナメントの保存・復元・一覧、display token 解決、イベントログと seq による replay。
- `internal/core/persist.go` とコア型の更新: タイマー状態、レベル構造、カウントを永続化し、指定クロックで復元。
- `internal/httpapi`: トーナメント作成・一覧・snapshot、start / pause / resume / adjust / next_level / prev_level / reset / counts コマンド、公開 display snapshot、SSE。
  - SSE は `id: <seq>` とイベント envelope を送り、`Last-Event-ID` より後の保存済みイベントを先に replay。15 秒間隔の heartbeat comment を送信。
  - 250ms ticker が running のトーナメントを進め、レベル変更と終了を保存・通知。
  - `New` は `core.Clock` を受け取るため、HTTP テストでも FakeClock を注入可能。
- `cmd/ponytimer/main.go`: SQLite を開き、永続状態を復元して HTTP API を起動。
- 追加・更新ファイル: `go.mod`, `go.sum`, `internal/core/{persist.go,snapshot.go,tournament.go}`, `internal/store/store.go`, `internal/httpapi/{commands.go,handlers.go,server.go,sse.go,server_test.go}`, `cmd/ponytimer/main.go`。
- 作業は VM 交換による中断をまたいで継続し、途中で writer も切り替わった。既存実装を確認した上で不足していた HTTP API テストを追加。

### テストとローカル検証

- `internal/httpapi/server_test.go`: FakeClock で pause の残り時間が受信時刻に凍結されること、SSE envelope のフィールドと型、`Last-Event-ID` による未受信イベント replay を検証。
- ローカル結果（Go 1.27.4）:

```
$ gofmt -l .          # 出力なし
$ go vet ./...        # 問題なし
$ go test ./...
?    github.com/yppp/ponytimer/cmd/ponytimer       [no test files]
ok   github.com/yppp/ponytimer/internal/core
ok   github.com/yppp/ponytimer/internal/httpapi
ok   github.com/yppp/ponytimer/internal/store
```

---

## Step C — アカウント認証と組み込み Web UI

### ビルドしたもの

- `internal/auth`: Argon2id による PHC 形式パスワードハッシュ（ランダム salt、定数時間比較）、初回 `/setup`、`/login` / `/logout`、セッション管理と認証 middleware。
  - Cookie は HttpOnly、SameSite=Lax、Path=/。TLS 接続では Secure。
  - 初回以降の公開登録はなく、追加アカウントはログイン済みの `/admin/accounts` から作成する。
  - `/api/tournaments*` は未認証時に JSON 401、`/admin` とアカウント管理ページは `/login` へリダイレクトする。
- `internal/store/accounts.go`: アカウントと期限付きセッションの保存・取得・削除。
- `internal/webui`: テンプレートと CSS / JavaScript をバイナリに埋め込み。管理画面の大会作成・カウンター・操作パネル、複数 token と 1/2/4 slot に対応する公開 display 画面。
  - display は `level_ends_at_ms` からブラウザー内で時計を進め、30 秒ごとに snapshot を再取得する。token 付き SSE で更新し、Waiting / Finished 表示と Break overlay を備える。
- `internal/httpapi`: 大会管理 API に認証 middleware を適用し、公開 display SSE を追加。
- `cmd/ponytimer/main.go`: 認証、Web UI、API の route を組み立てる。
- 追加・更新ファイル: `cmd/ponytimer/main.go`, `go.mod`, `go.sum`, `internal/auth/`, `internal/store/accounts.go`, `internal/webui/`, `internal/httpapi/{server.go,sse.go,server_test.go,auth_test.go}`。

### テストとローカル検証

- `internal/auth`: hash round-trip、異なる salt、誤パスワード・不正 hash の拒否、setup の一度限り、login/logout、Cookie 属性、アカウントページの認証を確認。
- `internal/httpapi/auth_test.go`: 未認証 API の JSON 401、認証済み API、公開 display snapshot を確認。
- ローカル結果（Go 1.27.4）:

```
$ gofmt -l .          # 出力なし
$ go vet ./...        # 問題なし
$ go test ./...
?    github.com/yppp/ponytimer/cmd/ponytimer       [no test files]
ok   github.com/yppp/ponytimer/internal/auth
ok   github.com/yppp/ponytimer/internal/core
ok   github.com/yppp/ponytimer/internal/httpapi
ok   github.com/yppp/ponytimer/internal/store
?    github.com/yppp/ponytimer/internal/webui       [no test files]
```

### 残課題 / スコープ上の確認事項

- `/setup` はアカウント数を確認してから最初のアカウントを作成するため、同時に複数の初回 setup が届いた場合の排他は DB トランザクションで保証していない。通常の逐次利用では 2 件目以降を拒否する。初回セットアップ競合の完全な防止は必要性を確認して別途扱う。
- ブラウザーを使った受け入れ smoke test は未実施。以下を coordinator が実行する。

#### Acceptance smoke test（coordinator が実行）

- [ ] setup account
- [ ] login
- [ ] create tournament
- [ ] open display
- [ ] start
- [ ] SSE updates
- [ ] pause freezes display

この実装は、最初の作業セッションが VM 交換で中断された後、writer の切り替えをまたいで継続した。

## Acceptance smoke test (coordinator-run, 2026-11-11)

Built the binary (`go build ./cmd/ponytimer`) and ran it against a fresh DB.
Verified end to end with curl:

- `GET /healthz` → ok. `POST /setup` creates the first account (303 → /admin);
  `POST /login` issues a session cookie.
- Unauthenticated `GET /api/tournaments` → 401.
- Create with the WSOP ME 2022 Appendix A opening levels (100/200, 200/300,
  200/400, 120 min each, starting stack 60,000) → id + display_token issued,
  state `waiting`.
- Counts command: fields take `{"value": n}` or `{"delta": n}` ops.
  entries=9, rebuys=2, players_left 9→7 via delta −2 → total_chips 330,000,
  average_stack 47,142.86 (matches hand calculation).
- Start → snapshot (via display token, no auth) shows state `running` and
  `level_ends_at_ms` set; remaining counts down from 7,200 s.
- SSE stream on `/api/tournaments/{id}/events` delivered `started` (seq 2)
  and `paused` (seq 3) events in the fixed envelope
  {seq, type, tournament_id, structure_version, server_time, payload}.
- Pause freeze: remaining_sec = 7197.926475399 immediately after pause and
  byte-identical 3 s later — the remainder is fixed at server receipt time
  (D6), not by display-side arrival.
- Restart recovery: killed and relaunched the server on the same DB file;
  snapshot still reports state `paused` with the identical remaining_sec.

Result: PASS (all items). Display split view (slots=2/4) is implemented in
the display page per Step C but was only code-verified here, not exercised
in a browser.
