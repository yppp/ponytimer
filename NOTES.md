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
  → 解決: ユーザー確認済み・仕様 §6.2 の式で確定（リエントリーは新規バイインとしてエントリーと同格に数える）。式自体は Step B 時点で §6.2 に一致済み。本ステップで式のテスト（リエントリーでアベレージが増えるケースを含む）、Snapshot への rebuy_stack 露出、管理画面のリバイスタック設定欄を追加して確定した。
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

## 総チップ量の定義を確定（ユーザー確認）

**決定**: 総チップ量は仕様 §6.2 の式で確定する。

```
Total chips = (entries + reentries) × starting stack
            + rebuys × rebuy stack
            + addons × addon stack
```

**理由**: リエントリーは新規のバイインであり、テーブルに新しいスタックを 1 つ追加する
行為なので、エントリーと同格に数える。リエントリーを除外すると、平均スタックも
R13 の終盤 BB 到達時の BB 推定も小さく見積もってしまう。リバイスタックは
開始スタックとは別の設定項目として保持する（未設定のときは開始スタックを使う）。

**このステップで追加したもの**:

- `internal/core/snapshot_test.go`: §6.2 の式を固定するテスト。
  - リエントリーが総チップ量とアベレージを増やすこと
    （entries=9, reentries=1, players_left=10, starting_stack=30000 → 300000、平均 30000）。
  - 同じフィールドで 1 人が busted して再エントリーした場合、総チップ量が
    開始スタックちょうど 30000 だけ増えること。
  - リバイスタック 10000（開始スタック 30000 とは別）で rebuys=2 が 20000 を寄与すること。
  - アドオンスタックが両方のスタックと別で寄与すること。
  - rebuy_stack が 0 のときは開始スタックにフォールバックすること。
- `internal/core/snapshot.go`: `Snapshot` に `rebuy_stack` を追加（`addon_chips` の隣）。
  追加のみの変更で、既存の JSON 契約は壊さない。
- `internal/webui`: 作成フォームに「Rebuy stack」欄を追加し、作成リクエストの
  `rebuy_stack` として送信する（空または 0 は「開始スタックと同じ」を意味する）。

---

## Major-1 修正 Step 1 — `tickAll` の遷移判定を RawState ベースに直す

### 根本原因

- `internal/httpapi/server.go` の `tickAll` が Tick 前後の**派生状態** `State()` を比較し、
  「状態が変わった＝トーナメント終了」とみなして `finished` イベントを発行・永続化していた。
- `State()` は Break 行の上にいる間 `Break` を返す派生値なので、
  通常レベル→Break 行で `running → break`、Break 行→通常レベルで `break → running` と
  状態変化が起き、**Break に入る瞬間と戻る瞬間の両方で誤った `finished` が保存された**。
- コア側の `RawState()`（`internal/core/tournament.go`）は Break 派生値を持たない内部状態を
  返し、「遷移を監視する呼び出し側は RawState を比較すること」とコメントがある。
  `tickAll` がそれを使っていなかったのが欠陥の直接原因。

### 修正内容

- `internal/httpapi/server.go` の `tickAll`:
  - before/after の比較を `rt.t.State()` から `rt.t.RawState()` に変更。
  - `finished` は「RawState が `Running` → `Finished` へ遷移したとき」だけ発行。
    遷移後は `IsRunning()` が false になり `tickAll` の対象から外れるため、構造上ちょうど1回だけ出る。
  - 最終行到達でないレベル変化（Break 行への出入りを含む）は従来どおり `level_changed` を発行。
  - 保存の順序と形は現行を維持: `SaveTournament` してから `emitLocked`、エラーは `log.Printf` で記録。
- `internal/core` は変更していない（`RawState()` は既存の公開 API のため追加対応不要）。

### ローカル検証結果（Step 1）

```
$ gofmt -l .          # 出力なし
$ go vet ./...        # 問題なし
$ go test ./...
?   	github.com/yppp/ponytimer/cmd/ponytimer	[no test files]
ok  	github.com/yppp/ponytimer/internal/auth
ok  	github.com/yppp/ponytimer/internal/core
ok  	github.com/yppp/ponytimer/internal/httpapi
ok  	github.com/yppp/ponytimer/internal/store
?   	github.com/yppp/ponytimer/internal/webui	[no test files]
```

- ticker 経路のテストは Step 2 で追加する（このステップでは既存テストのみ）。

---

## Major-1 修正 Step 2 — ticker 経路の統合テスト

### 追加したもの

- `internal/httpapi/server_test.go`
  - `testServer` を `testServerWithTick(t, tickInterval)` に一般化し、既存の `testServer(t)` は
     ticking 無効（interval 0）のラッパーにした。既存テストはそのまま通る。
  - `testEnv` に `store *store.Store` を追加し、テストから永続化済みのイベントログを読めるようにした。
- `internal/httpapi/ticker_test.go`（新規）
  - `TestTickerTransitions`: `TickInterval = 5ms` の実サーバーを起動し、
    構造 `level 60s / break 60s / level 60s` のトーナメントを HTTP API 経由で作成して `start` する。
  - 時刻制御は `core.FakeClock`。時計を進めた後、**永続化済みイベントログ**（`store.ListEventsAfter`）を
    deadline 5 秒でポーリングして待機する。固定 sleep による同期は使わない。
  - assert:
    1. Break 行へ入った時点: `level_changed` が 1 件、`finished` は 0 件。
    2. Break から通常レベルへ戻った時点: `level_changed` が 2 件、依然 `finished` は 0 件。
    3. 最終行を終えた時点: `finished` がちょうど 1 件。
       さらに別の probe トーナメントを走らせて「ticker が今も動いている」ことを示し、
       その後も `finished` の総数が 1 のままであることを確認する。
  - イベント種別の列（`created` / `started` を除く ticker 由来分）が
    `level_changed, level_changed, finished` の順であることも確認する。

### テストが確実にバグを捕まえることの確認

- Step 1 の修正前の `internal/httpapi/server.go`（`git show 7d60be7:internal/httpapi/server.go`）を
  一時的に戻して新テストを実行すると、期待どおり失敗することを確認した:
  ```
  ticker_test.go:81: level_changed after entering the break row = 0, want 1 (events [created started finished])
  --- FAIL: TestTickerTransitions (0.31s)
  ```
  つまり Break 行への進入が `finished` として記録される旧バグを新テストが検出する。

### ローカル検証結果（Step 2）

```
$ gofmt -l .          # 出力なし
$ go vet ./...        # 問題なし
$ go test ./...
?   	github.com/yppp/ponytimer/cmd/ponytimer	[no test files]
ok  	github.com/yppp/ponytimer/internal/auth
ok  	github.com/yppp/ponytimer/internal/core
ok  	github.com/yppp/ponytimer/internal/httpapi
ok  	github.com/yppp/ponytimer/internal/store
?   	github.com/yppp/ponytimer/internal/webui	[no test files]
```

- テスト件数: 37 件（auth 10 / core 16 / httpapi 4 / store 4 + webui・cmd はテストなし）。
- flaky でないことの確認: `go test ./internal/httpapi/ -run TestTicker -race -count=10` が全 PASS。

---

## 表示改善 Step 1 — 現在レベルの強調表示

対象: `internal/webui` の表示側のみ（サーバー側 API / core / httpapi は変更なし）。

### 変更したファイル

- `internal/webui/static/display.js`
  - スロットごとに独立したレベル表示要素 `.slot-level` を追加し、`.slot-main`（レベル・タイマー・
    メッセージ・ブラインドをまとめる箱）を新設。
  - `setLevel()` を追加: `snapshot.current_level.index` は 0-based なので表示は `index + 1`。
    `current_level` が null のときは要素を空のままにして `display:none` にし、誤った番号を出さない。
  - Break overlay にも `.overlay-level` を追加し、同じ `LEVEL n` を出す。Break 行自体が現在行なので
    ここも `current_level.index + 1`（Break 行の番号）を使う。
- `internal/webui/static/style.css`
  - `.slot` を「上（名前・絶対配置）／中央（`.slot-main`）／下（統計）」の 3 段 flex に再構成。
    `.slot-main` が `flex:1` で空きを吸収するので、レベル要素は名前・統計と重ならず、
    タイマーは空き領域の中央に留まる。
  - `.slot-level` は `clamp(22px, 6vh, 64px)` の太字・アクセント色（`--accent`）で、
    タイマー/ブラインドと同じ情報階層に置いて明示する。分割 4 では `clamp(16px, 4.5vh, 48px)`。
  - `.overlay-level` を追加（分割 4 では `clamp(16px, 4.5vh, 48px)`）。
  - 既存 `data-slots=4` の調整（padding / gap / 各フォント）を新レイアウトに合わせて更新。

### 検証結果（Step 1）

```
$ gofmt -l .          # 出力なし
$ go vet ./...        # 問題なし
$ go test ./...
?   	github.com/yppp/ponytimer/cmd/ponytimer	[no test files]
ok  	github.com/yppp/ponytimer/internal/auth
ok  	github.com/yppp/ponytimer/internal/core
ok  	github.com/yppp/ponytimer/internal/httpapi
ok  	github.com/yppp/ponytimer/internal/store
?   	github.com/yppp/ponytimer/internal/webui	[no test files]
```

- テスト件数: 37 件（webui / cmd はテストなし）。JS の構文は `node --check static/display.js` で確認。
- ブラウザ実機での目視確認は未実施（この環境にはヘッドレスブラウザが無い。Step 4 のNOTE参照）。

---

## 表示改善 Step 2 — 統計（プレイヤー数 / エントリー数 / 平均スタック）の格上げ

対象: `internal/webui` の表示側のみ（サーバー側 API / core / httpapi は変更なし）。

### 変更したファイル

- `internal/webui/static/display.js`
  - 1 行に押し込んでいた `statsText()` をやめ、`statsHTML()` でラベル付きの項目として描画。
    - `PLAYERS` = `players_left`
    - `ENTRIES` = `entries`
    - `AVG STACK` = `average_stack`（既存どおり `Math.round` 後に `fmt` で桁区切り）
  - 副次項目（Rebuys / Addons / Reentries）は `.stat-extra` の小さい補助行に残す。
    0 件のときは既存の趣旨どおり行ごと出さない。
  - 値を捏造せず snapshot の値をそのまま使う。開始前の `average_stack` も snapshot の値を出し、
    フロント側で starting stack から計算しない。
  - `waiting`（"Waiting To Begin"）と `finished`（"Finished"）でも統計を表示する。
    メッセージは従来どおり維持し、統計は消さない。
  - `running` / `paused` / `break` でも同じ統計領域を使う。Break 中は overlay がスロットを覆うため
    overlay 側に統計は複製しない（-overlay を閉じた通常レイアウトで正しく描画される）。
- `internal/webui/static/style.css`
  - `.slot-stats` を flex 列（主項目行 + 補助行）にし、`.stat-row` は `flex-wrap: wrap`、
    `.stat` はラベル/値の縦組み。ラベルは 0.5em・`letter-spacing: 0.14em`（`text-indent` で字面の中央を補正）、
    値は 1em・太字・`tabular-nums`。
  - 分割 4 では `.slot-stats` のフォントを `clamp(10px, 1.9vh, 22px)` に縮め、
    `.stat-row` は折り返し可能なので 3 項目が潰れずに並ぶ／折り返される。

### 検証結果（Step 2）

```
$ gofmt -l .          # 出力なし
$ go vet ./...        # 問題なし
$ go test ./...
?   	github.com/yppp/ponytimer/cmd/ponytimer	[no test files]
ok  	github.com/yppp/ponytimer/internal/auth
ok  	github.com/yppp/ponytimer/internal/core
ok  	github.com/yppp/ponytimer/internal/httpapi
ok  	github.com/yppp/ponytimer/internal/store
?   	github.com/yppp/ponytimer/internal/webui	[no test files]
```

- テスト件数: 37 件。JS の構文は `node --check static/display.js` で確認。
- この環境にはヘッドレスブラウザが無いため、DOM を最小スタブして `display.js` を実行する
  スモークテスト(`/tmp` の一時スクリプト、コミット対象外)を作成し、running / paused / break /
  waiting / finished の各状態で描画される HTML を確認した。
  - 教訓: この過程で `statsText()` への呼び出し残し（未定義関数による例外）が 1 件見つかり、
    実行時エラーで統計が空になる状態だったため `statsHTML()` 呼び出しへ修正済み。
  - 確認できた内容: `LEVEL 3`（index+1）、`PLAYERS 87` / `ENTRIES 120` / `AVG STACK 13,448` が
    5 状態すべてで描画されること、Rebuys/Addons/Reentries が 0 のとき補助行が出ないこと、
    1 件以上あるときだけ補助行が出ること。

---

## 表示改善 Step 3 — フルスクリーンボタンと Wake Lock

対象: `internal/webui` の表示側のみ（サーバー側 API / core / httpapi は変更なし）。

### 変更したファイル

- `internal/webui/templates/display.html`
  - ページ全体に 1 つだけ常設するフルスクリーンボタン（`#fullscreen`）を追加。
    スロット数（1/2/4），不管，始终 1 つだけ。
    `aria-pressed="false"` / `aria-label="Enter fullscreen"` / `title` / 文言で全画面操作だと分かる。
- `internal/webui/static/display.js`
  - `toggleFullscreen()`: fullscreen でなければ `document.documentElement.requestFullscreen()`、
    fullscreen 中なら `document.exitFullscreen()`。どちらも Promise の rejection は握り潰し、
    API が無い場合は何もしない（例外はページへ漏らさない）。
  - `updateFullscreenButton()`: `fullscreenchange`（および webkit 版）でボタンの文言・`aria-pressed`・
    `aria-label`・`title` を現在の状態に追従させる。
  - `requestWakeLock()`: `navigator.wakeLock` があれば best effort で `screen` ロックを要求する。
    失敗・拒否・例外はすべて握り潰し、ログにも出さない。ロックは `release` イベントで片付ける。
    起動時・ユーザー操作（クリック/キー操作）時・`visibilitychange` で visible に戻った時に再取得を試みる。
- `internal/webui/static/style.css`
  - `.fullscreen-btn`: `position: fixed` の右下（`right/bottom: 12px`, `z-index: 30`）、
    控えめ（`opacity: 0.6`）だが十分なタップ領域（`min-height: 44px`）、hover/focus で濃くする。
    `aria-pressed="true"`（全画面中）はアクセント色で塗り、状態が目で分かる。

### 検証結果（Step 3）

```
$ gofmt -l .          # 出力なし
$ go vet ./...        # 問題なし
$ go test ./...
?   	github.com/yppp/ponytimer/cmd/ponytimer	[no test files]
ok  	github.com/yppp/ponytimer/internal/auth
ok  	github.com/yppp/ponytimer/internal/core
ok  	github.com/yppp/ponytimer/internal/httpapi
ok  	github.com/yppp/ponytimer/internal/store
?   	github.com/yppp/ponytimer/internal/webui	[no test files]
```

- テスト件数: 37 件。JS の構文は `node --check static/display.js` で確認。
- DOM をスタブした一時スクリプト（`/tmp`、コミット対象外）でフルスクリーンの経路を確認した:
  - ボタン初期表示 `⛶ FULLSCREEN` / `aria-pressed=false`
  - クリック → `requestFullscreen` 呼び出し（1 回）→ `fullscreenchange` 後に
    `⛶ EXIT FULLSCREEN` / `aria-pressed=true` / `aria-label="Exit fullscreen"`
  - もう一度クリック → `exitFullscreen` 呼び出し → 復帰後に `⛶ FULLSCREEN` / `aria-pressed=false`
  - Wake Lock は `navigator.wakeLock.request()` が reject する想定で、**未処理の Promise rejection は 0 件**
    （reject を握り潰していることを確認）。

---

## 表示改善 Step 4 — スマホ縦画面（portrait）向けレスポンシブ対応

対象: `internal/webui` の表示側のみ（サーバー側 API / core / httpapi は変更なし）。

### 変更したファイル

- `internal/webui/static/style.css`
  - 縦画面用のルールを `@media (orientation: portrait), (max-width: 560px)` の中に閉じ込めた。
    横画面・TV（16:9）の既存レイアウトには一切触れていない（既存の `data-slots` 調整もそのまま尊重）。
  - `.slots` に `height: 100dvh` を追加（未対応ブラウザは上の `100vh` を使う。スマホの
    アドレスバー追従のため）。
  - 縦画面では `.slots` に `padding-bottom: 56px` を確保し、右下のフルスクリーンボタンと
    下段スロットの統計が重ならないようにした（横画面は `padding-bottom` なしで従来どおり）。
  - タイマーを `vw` ベースの `clamp()` にして viewport 幅に追従:
    - 分割 1: `clamp(48px, 21vw, 190px)`（390px 幅で 82px）
    - 分割 2（縦積み 1列×2行）: `clamp(44px, 17vw, 140px)`
    - 分割 4（2列×2行）: `clamp(28px, 11vw, 80px)`
    - LEVEL / ブラインド / メッセージ / overlay も同じ思路で `vw` ベースに変更。
  - 縦画面の分割 2 は `grid-template-columns: 1fr; grid-template-rows: 1fr 1fr;`（縦積み）。
    分割 4 は 2列×2行。
  - 縦画面の分割 4 では統計の 3 項目が 1 行に収まらないため `.stat-row` を縦積み、
    `.stat` はラベルと値を横に並べる（省略や画面外への脱落なし）。
  - 分割 4（横画面）の統計を `clamp(11px, 2.2vh, 26px)` に上げ、遠目でも読めるようにした。
- `internal/webui/static/display.js`
  - Break 中は overlay がスロット内の level / time / blinds と重なって二重に見えていたため、
    overlay 側で同じ内容(Level / 残り時間 / Next) shown する間は裏側の 3 つを空にする。
    （overlay の LEVEL 表示は Step 1 のとおり Break 行の番号。）
- `internal/webui/webui_test.go`（新規）
  - `TestDisplayAssets`: 埋め込み済み|display.html / display.js / style.css に、
    レベル表示・統計 3 項目・フルスクリーン操作・portrait メディアクエリがあることを確認する
    静的スモークテスト。ブラウザが無い CI でも表示の要が抜け落ちないことを保証する。

### 検証結果（Step 4）

```
$ gofmt -l .          # 出力なし
$ go vet ./...        # 問題なし
$ go test ./...
?   	github.com/yppp/ponytimer/cmd/ponytimer	[no test files]
ok  	github.com/yppp/ponytimer/internal/auth
ok  	github.com/yppp/ponytimer/internal/core
ok  	github.com/yppp/ponytimer/internal/httpapi
ok  	github.com/yppp/ponytimer/internal/store
ok  	github.com/yppp/ponytimer/internal/webui
```

- テスト件数: 38 件（webui に 1 件追加）。

#### 描画確認（headless Chrome、実機代わりに実施）

この環境にはヘッドレスブラウザが無かったため、**リポジトリ外（`/tmp`）に一時的に
puppeteer + Chromium をインストール**して、表示ページ（fake snapshot を返すローカル HTTP サーバ経由）
を実際に描画した。プロジェクト側の依存には一切入れていない（`go.mod` / `package.json` の変更なし）。

確認した組み合わせと結果（すべて「横はみ出しなし・要素のスロット外はみ出しなし・
統計とフルスクリーンボタンの重なりなし」）:

| 状態 | ビューポート | 分割 | 結果 |
|---|---|---|---|
| running | 390x844 | 1 | ok |
| running | 390x844 | 2（縦積み） | ok |
| running | 390x844 | 4（2x2） | ok |
| running | 360x800 | 4（2x2） | ok |
| running | 430x932 | 2（縦積み） | ok |
| running | 1920x1080 | 1 / 2 / 4 | ok |
| waiting | 390x844 | 1 | ok |
| break | 390x844 | 1 | ok |

- 判定はスクリプトで機械的に行った: `document.documentElement.scrollWidth <= innerWidth`（横はみ出しなし）、
  各スロットの子要素（level / time / message / blinds / stats / name）の bounding box が
  スロットの矩形内であること、統計の実コンテンツ（`.stat-row` / `.stat-extra`）が
  フルスクリーンボタンの矩形と交わらないこと。
- この過程で 1 件实际问题を発見して修正した: Break 中に overlay の LEVEL/タイマーと
  スロット裏側の LEVEL/タイマーが重なって二重に見えていた（overlay 側を空にする修正）。
- スクリーンショットは `/tmp/pt-check/shots/` に置いた（リポジトリには入れていない）。
- **実機（iPhone/Android の実ブラウザ）での目視確認は未実施**。headless Chrome の描画結果と
  数値判定による確認まで。実機（iPhone / Android の Safari, Chrome）では
  Safari の `dvh` 対応差やノッチ領域の見え方は別途確認が要る。

### この 4 ステップ全体について

- 変更範囲は `internal/webui` のみ（templates/display.html, static/display.js, static/style.css,
  webui_test.go の追加）。サーバー側 API / internal/core / internal/httpapi は変更していない。
- 分割 1 / 2 / 4 と縦横双方のレイアウトを headless Chrome で確認済み。

## 表示改善 Step 5 — portrait の複数スロットを横長帯に変更

- 実装担当の経緯: 本来の書き手 FCC (MiniMax) がこのステップで3回失敗
  （NIM応答が16分停止、基盤サービス再起動でセッション消滅、初回応答が8分以上未完）したため、
  AGENTS.md のルーティングに基づき Codex がフォールバックで実装した。
- `internal/webui/static/style.css`
  - portrait の分割 2 は1列×2行を維持しつつ、分割 2 / 4 の各 slot を横長帯に変更。
    左に残り時間（待機・終了時はメッセージ）、右に LEVEL / ブラインド / 統計を配置する。
  - portrait の分割 4 は1列×4行。単一スロット、横画面 / TV のルールは変更していない。
  - 主要統計 PLAYERS / ENTRIES / AVG STACK は両方の帯で残し、ラベルと値を横並びにしている。
    分割 4 は `.stat-extra`（Rebuys / Addons / Reentries）を非表示にした。帯の高さが画面の約1/4しかなく、
    副次行を残すと主要3項目の文字を読みにくいサイズまで落とす必要があるため。主要3項目は全て残す。
- `internal/webui/webui_test.go`
  - `TestDisplayAssets` で portrait メディアクエリ内の分割 4 が1列指定であることを確認する。
- 描画確認: 実装時、`/tmp/pt-check/` が存在せず Codex は再インストールしなかったため未実施だったが、
  コミット前にコーディネーターが `/tmp/pt-check/` に puppeteer + Chromium を再構築して実施した。
  fake snapshot（paused、LEVEL 4、AVG STACK 133,333、副次項目あり）を返すローカルサーバ経由で、
  390x844 の分割 2 / 4 ともに PASS: スロットが縦1列に積まれ、時間の中心が LEVEL の中心より左にあり
  （中心 x = 91 対 279）、主要統計3項目が全て表示され、統計行のはみ出し・横スクロールなし。
  分割 4 では `.stat-extra` が非表示であることも確認。390x844 分割 1 と 1920x1080 分割 2 / 4 も PASS
  （横画面の退行なし）。実機（iPhone / Android）の目視確認は未実施。
- 検証結果: `gofmt -l .` は出力なし、`go vet ./...` は問題なし、`go test ./...` は全パッケージ PASS。
  既定の Go build cache は読み取り専用だったため、`GOCACHE=/tmp/pt-go-cache` を指定して vet / test を実行した。

## Issue #4 — データモデル拡張 (format / bets / ゲームミックス)

v0.1 レビューの Major-2 対応。レベル行に format / bets / game を持たせ、
仕様 D9 / D10 (§4.3 / §7.7 / §11) のミックスゲーム強制ベット表示を表現可能にする。

### 設計メモ (実装前、コーディネーター作成)

#### モデルの最終形 (internal/core)

- `Bets` 構造体を新設する。全フィールド `omitempty`:
  `sb`, `bb`, `big_bet`, `ante`, `bring_in`, `complete` (すべて int)。
- format 定数: `FormatBlindNL = "blind-nl"`, `FormatBlindLimit = "blind-limit"`,
  `FormatStudLimit = "stud-limit"`。
- `Level` からフラットな `SB` / `BB` / `Ante` を外し、
  `Game string (json:"game,omitempty")`、`Format string (json:"format,omitempty")`、
  `Bets Bets (json:"bets")` を持たせる。Break 行は game / format 空、bets ゼロ。
- `Level.EffectiveFormat()`: 空なら `blind-nl` を返す。
- `GameMix` 構造体を新設する (§4.3 / §11):
  `games []string`, `stud_enabled bool`, `stud_show_big_bet bool`,
  `levels_per_game int`。`Tournament.GameMix` として保持する。
  ローテーション自体の本式実装は後段で、ここでは設定値の保持のみ。
  `levels_per_game` が 0 以下のときの実効値は 1。
- スナップショット: `LevelView` は `{index, type, game, format, bets, duration_sec}`
  とし、フラットな sb/bb/ante は廃止する。level 行の `format` は実効値
  (空にしない) を出す。`Snapshot` に `game_mix` を追加する。

#### 永続化の移行 (後方互換が必須)

- `Level` にカスタム `UnmarshalJSON` を付け、旧形式
  (行に `sb` / `bb` / `ante` がフラット、`format` 無し) と新形式の両方を受ける。
  旧形式は `bets` へ畳み込み、`type=level` で format が空なら `blind-nl` を補う。
  両形式が混在したら `bets` 側を優先する。
- 書き出しは新形式のみ (カスタム Marshal は不要)。
- `PersistedState` に `game_mix` を追加する。旧 JSON に無い場合はゼロ値
  (= スタッド無効、levels_per_game 実効 1) として読めること。
- 移行テスト必須:
  1. 旧形式の永続 JSON (フラット sb/bb/ante、format 無し、game_mix 無し) を
     `PersistedState` 経由で復元し、bets への畳み込みと format 補完を確認する。
  2. 移行後の marshal が新形式 (bets / format を含み、行にフラット sb を含まない)
     になることを確認する。
  3. 新形式 (stud 行: bring_in / complete) がそのまま往復することを確認する。

#### API (internal/httpapi)

- 作成リクエストに `game_mix` を追加する。
- バリデーション: 既存の type / duration チェックに加え、format が
  空 / blind-nl / blind-limit / stud-limit のいずれかでなければ 400。
  API が旧形式 (フラット) のレベル JSON を受けても移行経路で通ること。

#### BIG BET の導出 (表示側の規則、§7.7)

- blind-limit: `bets.big_bet` があればそれ、無ければ BB x 2 を表示する。
- stud-limit: 既定では BIG BET を表示しない。`game_mix.stud_show_big_bet` が
  true のときだけ、`bets.big_bet` があればそれ、無ければ COMPLETE x 2 を表示する。
- 値が 0 / 欠けた項目はラベルごと省略する (空ラベルを残さない)。

#### 管理画面の入力記法 (internal/webui/static/admin.js)

- 旧記法は不変: `sb,bb,ante,minutes` は blind-nl、`break,minutes` は Break。
- レベル行は空白区切りのプレフィックス `game=<名前>` / `fmt=<format>`
  (順不同、両方任意) を付けられる。カンマ列の形は format で決まる:
  - blind-nl: `sb,bb,ante,minutes`
  - blind-limit: `sb,bb,big_bet,ante,minutes` (big_bet は空または `-` で 0 = 導出)
  - stud-limit: `bring_in,complete,ante,minutes`
  - 例: `fmt=blind-limit 100,200,400,0,20`、`game=Stud fmt=stud-limit 25,100,25,20`
- 作成フォームにゲームミックス設定を追加する: スタッド有効チェックボックス、
  スタッド BIG BET 表示チェックボックス、levels_per_game (数値、既定 1)、
  games (カンマ区切りテキスト)。作成リクエストの `game_mix` として送る。
- テキストエリア下のヘルプ文を新記法に合わせて更新する。
- 管理画面の現在/次レベル表示 (`describe`) は snapshot の bets / format から
  形式別のラベルで描く。game があれば名前も出す。

#### 表示画面 (internal/webui/static/display.js)

- PR #13 のレイアウト (LEVEL 表示、統計、縦積み帯) は壊さない。変更は
  ブラインド表示部分と game 名の表示のみ。
- `blindsHTML` / `nextBlindsText` を format で切り替える:
  - blind-nl: SB / BB / ANTE
  - blind-limit: SB / BB / BIG BET / ANTE
  - stud-limit: BRING-IN / COMPLETE / ANTE (+ 条件付き BIG BET、上記導出規則)
  - format が無い / 空の場合は blind-nl として扱う (旧スナップショット互換)。
- 現在レベルの game 名があれば LEVEL 表示の近くに見えるようにする
  (例: `LEVEL 3 — NLHE`)。Next 表示にも next の game があれば含める。
  Break オーバーレイの Next 表示も同じ経路で切り替わること。

#### スコープ外 (この Issue ではやらない)

- ローテーション (levels_per_game での自動 game 切替) の本式実装。
- 表計算エディタ本体 (#6)。
- stud_enabled=false のときに stud 行を弾くバリデーション (エディタ側の領分)。

### 実装メモ (Step A: core モデル + 移行 + テスト)

- 変更ファイル:
  - `internal/core/level.go`: `Bets` 構造体 (sb / bb / big_bet / ante / bring_in / complete、
    すべて int + omitempty)、format 定数 3 種、`Level` を
    `{index, type, game, format, bets, duration_sec}` へ変更。
    `Level.EffectiveFormat()` (空 → blind-nl) と `core.ValidFormat()` を追加。
    `Level.UnmarshalJSON` を実装: 旧形式 (フラット sb/bb/ante) を bets へ畳み込み、
    `type=level` で format が空なら blind-nl を補う。bets がある行は bets 優先。
    書き出しは新形式のみ (カスタム Marshal は無し)。
  - `internal/core/gamemix.go` (新規): `GameMix` 構造体
    (games / stud_enabled / stud_show_big_bet / levels_per_game) と
    `EffectiveLevelsPerGame()` (0 以下 → 1)。
  - `internal/core/tournament.go`: `Tournament.GameMix` を追加。
  - `internal/core/persist.go`: `PersistedState.GameMix` を追加。旧 JSON に
    game_mix が無い場合はゼロ値として読める (スタッド無効 / 実効 1 レベル)。
  - `internal/core/snapshot.go`: `LevelView` を `{index, type, game, format, bets,
    duration_sec}` へ変更 (フラット sb/bb/ante は廃止)。level 行の format は
    実効値、break 行は format 空。`Snapshot.GameMix` を追加。
  - `internal/core/level_test.go` (新規): 移行テスト 1〜3 (旧永続 JSON の復元、
    移行後の新形式書き出し、新形式の stud 行の往復) ＋ bets 優先・実効 format・
    ValidFormat・snapshot の game_mix / format 確認。
  - `internal/core/tournament_test.go`, `internal/store/store_test.go`:
    旧フラット字段の前提を新モデルへ更新 (検証内容は変えない)。
- 決めたこと:
  - `LevelView.Format` は level 行では必ず実効値 (空にしない)、break 行は空
    (break には強制ベットも形式もない)。
  - `Level.Format` はメモリ上でも空のままでよい (実効値は EffectiveFormat で
    取る)。JSON 往復後だけ migration が blind-nl を埋めるため、store の往復
    テストは構造に format を明示した。
  - `Bets` の json タグは `bets` (omitempty なし)。break 行は `{"bets":{}}`。
  - `GameMix` の `games` のみ omitempty。他は常に JSON に出る。
- テスト結果: `gofmt -l .` は出力なし、`go vet ./...` は問題なし、
  `go test ./...` は全パッケージ PASS。既定の Go build cache が読み取り専用の
  ため `GOCACHE=/tmp/pt-go-cache` を指定して実行した。

### 実装メモ (Step B: store / API / スナップショット)

- 変更ファイル:
  - `internal/httpapi/handlers.go`: 作成リクエストに `game_mix` を追加し
    `Tournament.GameMix` に反映。バリデーションに format の検査を追加
    (`core.ValidFormat`: 空 / blind-nl / blind-limit / stud-limit 以外で 400)。
  - `internal/httpapi/create_test.go` (新規): 旧形式 (フラット sb/bb/ante、
    format 無し) のレベル JSON が作成 API を通ることを確認 (bets への畳み込みと
    format=blind-nl の補完を snapshot で検証)。game_mix + 3 形式の作成テスト、
    不正な format の 400 テスト。
  - `internal/store/store_test.go`: 往復テストに game_mix と stud 行
    (bring_in / complete / ante) を追加。
- 決めたこと:
  - 旧形式のレベル JSON は API のデコード時に core の migration を通るため、
    API 側に特別な後処理は無い (core 側の責務)。
  - format の不正は「level %d has an unknown format ...」という行番号付き
    エラーで 400。
- テスト結果: `gofmt -l .` は出力なし、`go vet ./...` は問題なし、
  `go test ./...` は全パッケージ PASS (`GOCACHE=/tmp/pt-go-cache`)。

### 実装メモ (Step C: 管理画面の入力拡張)

※ 本ステップのコードは前任コーディネーターのセッションで書かれ、セッション中断により
未コミットのまま作業ツリーに残っていたものを、継続コーディネーターが精査・検証して
コミットした。内容は設計メモどおりで、修正なしで採用した。

- 変更ファイル:
  - `internal/webui/static/admin.js`: `parseLevels` を `parseLevel` に分割し、
    行プレフィックス `game=<名前>` / `fmt=<format>` (順不同・任意) に対応。
    カンマ列は format で切替 (blind-nl: sb,bb,ante,minutes /
    blind-limit: sb,bb,big_bet,ante,minutes (big_bet の空・`-` は 0 = 導出) /
    stud-limit: bring_in,complete,ante,minutes)。旧記法はそのまま blind-nl に通る。
    出力は新形式 `{type, format, bets, game?, duration_sec}` (break は bets 無し)。
    `gameMix()` を追加し、作成リクエストに `game_mix` を含める。
    `describe` / `describeLevel` を snapshot の bets / format ベースに書き換え、
    BIG BET の導出 (blind-limit は BB×2、stud は `stud_show_big_bet` のときだけ
    COMPLETE×2) と game 名の表示に対応。
  - `internal/webui/templates/admin.html`: 新記法のヘルプ文とゲームミックス設定欄
    (games テキスト、levels_per_game 数値、stud_enabled / stud_show_big_bet
    チェックボックス) を追加。
- 検証 (継続コーディネーター実施):
  - `node --check` OK。parseLevels を抽出して実行するハーネスで 8 ケース確認:
    旧記法 (フラット + break)、blind-limit の big_bet 導出/明示、stud + game 前置、
    前置の順序入替、不正な format / 列数 / 前置のエラー。
  - `gofmt -l .` 出力なし、`go vet ./...` 問題なし、`go test ./...` 全パッケージ PASS。

### 実装メモ (Step D: 表示画面の format 切替)

※ 書き手の経緯: FCC (MiniMax M3.1 Flash) は本ステップで 3 回失敗した
(1 回目: NIM が HTTP 503 "content safety screening is temporarily unavailable"
を返した後セッション停止、2 回目: 初回応答が 8 分以上来ずファイル変更ゼロ、
3 回目: 起動直後に同じ 503 で即時終了)。フォールバックの Codex もこの環境では
`codex-code-mode-host` バイナリ欠落のため実行自体ができなかった (v0.1 レビュー時と
同じ既知のブロッカー)。両経路が塞がれているため、本ステップに限り継続
コーディネーターが設計メモどおりに実装した。

- 変更ファイル:
  - `internal/webui/static/display.js`:
    - `betsOf` / `betParts` / `bigBetValue` / `pushBet` を追加し、`blindsHTML` と
      `nextBlindsText` を snapshot の bets + format で組み立てるよう変更。
      blind-nl: SB/BB/ANTE、blind-limit: SB/BB/BIG BET/ANTE (BIG BET は
      bets.big_bet、無ければ BB×2)、stud-limit: BRING-IN/COMPLETE/ANTE
      (BIG BET は game_mix.stud_show_big_bet のときだけ、bets.big_bet、
      無ければ COMPLETE×2)。0 / 欠けた項目はラベルごと省略。管理画面
      admin.js の describeLevel と同じ規則。
    - 後方互換: format が空なら blind-nl として扱い、`bets` が無い旧
      スナップショットではフラットな sb/bb/ante にフォールバックする。
    - `setLevel` は game があれば「LEVEL 3 — NLHE」形式にし、`nextBlindsText`
      も next の game 名を含める。Break overlay の Next も同じ経路。
    - CSS は変更なし (PR #13 のレイアウトは不変)。
  - `internal/webui/webui_test.go`: `TestDisplayAssets` の display.js 含有確認に
    "BIG BET" / "BRING-IN" / "COMPLETE" を追加。
- 検証 (コーディネーター実施):
  - `node --check` OK。関数を抽出するハーネスで 10 ケース確認: blind-nl、
    blind-limit の BIG BET 導出/明示、stud の BIG BET 入/切、旧形式
    スナップショット、next の game 名、next が break、現在ブラインドの
    HTML 結合、桁区切り。
  - `gofmt -l .` 出力なし、`go vet ./...` 問題なし、`go test ./...` 全パッケージ PASS。

## Issue #6 — ストラクチャー・エディタ

### 設計メモ (実装前、コーディネーター作成)

#### 構造 (Structure) の扱い: 独立エンティティ化しない

- 現行どおり Tournament がレベル列を保持し、エディタは「トーナメントの
  レベル列」を編集対象とする。仕様 §11 の Structure は「行の配列 + 版」と
  いう形の定義であり、`Tournament.Structure` + `StructureVersion` が
  実質的にそれを満たす。独立 ID 付きエンティティにすると永続化
  (state_json)・作成 API・スナップショットの全面移行が要る割に、
  v1 の利用者価値が無い。
- テンプレートだけは独立エンティティにする。別トーナメントへ適用する
  ものなので Tournament の外に存在する必要がある。新規テーブル
  `templates (id, name, levels_json, created_at, updated_at)` に
  名前 + レベル列で持つ (§11 の Template = Structure と同形)。
- ネストは「テンプレート (または他トーナメントの構造) の区間を
  編集バッファへ差し込む」操作としてエディタ側で合成する。
  サーバー側に合成 API は作らない。

#### 検証・シミュレーションの置き場所: Go の core に一本化

- `internal/core` に新設する:
  - `ladder.go`: マスター梯子と「綺麗な組」の判定 (下記)
  - `validate.go`: `ValidateStructure(levels, opts) []Issue`
    (Issue は severity: error / warn / info、行番号、rule、message)
  - `simulate.go`: `Simulate(levels, entries, startingStack, start)`
    → 総所要時間、終了予定時刻、レイト締切、行ごとの開始オフセットと
    アベレージ BB、R13 終了想定レベルと到達時刻、次レベル候補
- HTTP は `POST /api/structures/analyze` (要認証) が
  {levels, entries, starting_stack, start_time_ms, chip_unit,
  suggest_after_index} を受けて {issues, simulation, suggestions} を返す。
  エディタは編集のたびにデバウンスして呼び、検証とシミュレーションの
  正を Go 側に一本化する (#7 の生成と共有するため)。JS 側に検証
  ルールの複製は作らない。
- 累計時刻の列表示だけは行 duration の単純合計なので JS で計算してよい
  が、シミュレーションパネルとグラフは analyze の結果を使う。

#### 「綺麗な組」の形式化 (付録 B7 対応)

- 梯子値: 仮数部 {1, 1.2, 1.5, 1.6, 2, 2.5, 3, 4, 5, 6, 8} × 10^n。
  1.6 は旧主流形 (B2) のため梯子に含めるが legacy 扱いとする。
- 正規ペア表 (BB 仮数部 → 許容 SB 仮数部) を付録 A の実績 + B2 から作る:
  1→{0.5}, 1.2→{0.6}, 1.5→{1.0}, 1.6→{0.8}, 2→{1.0}, 2.5→{1.0, 1.25},
  3→{1.5, 2}, 4→{2}, 5→{2.5, 3}, 6→{3}, 8→{4}
- 判定: BB が梯子値でない → warn `not_clean_value`。組が正規ペア表に
  無い → warn `not_clean_pair`。警告には最も近い正規の組を提案として
  含める (例: 700/1400 → 1000/1500 への変更提案、§8.4)。
- これで B1 / B2 / B3 は無警告、B4 (700/1400) / B5 (800/1500) /
  B6 (700/1400 を含む) に警告が出る。B6 の「刻みすぎ」自体は
  700/1400 の警告でカバーされるものとし、上昇率が小さい区間への
  追加警告は本 Issue では必須としない。

#### 検証ルール一覧 (§8.4 + §14)

- error (保存不可): duration ≤ 0、レベル列が空、未知の format
- warn: BB の単調増加違反 (横ばい・減少)、SB > BB、SB == BB
  (§8.4: 意図的な SB=BB は警告のみ)、BB ジャンプ 2 倍超、
  最小チップ単位で割り切れない値 (chip_unit、既定 25)、
  綺麗でない値・組 (上記)、Ante > BB
- warn (§14): スタッド行の Ante < Bring-in < Complete 違反、
  Complete の単調増加違反、Limit 行で big_bet 明示値 ≠ BB×2
- info (§14): 形式が切り替わる境界での実効リミット跳ね上がり率
  (ブラインド系は BB、スタッド系は Complete を実効値とする)

#### シミュレーション (§8.4)

- 総チップ T = 想定エントリー数 × 開始スタック (リバイ等を含めない
  想定値であることを UI に明記する)。
- 行ごと: 開始オフセット (累計分)、アベレージスタック (= T / エントリー数)
  をその行の BB で割った BB 換算値。スタッド行は Complete を分母にする。
  Break 行は換算なし。
- 総所要時間 = 全行の duration 合計。終了予定時刻 = 開始時刻 + 総所要時間。
- レイト締切: marker `late_reg_end` の行が「そのレベルまで登録可」。
  締切レベル (その行の番号) と締切時刻 (その行の終了時刻) を返す。
- R13 終了想定: BB_end = T / 20。ブラインド行のうち BB が BB_end 以上に
  なる最初の行を終了想定レベルとし、その開始時刻を到達時刻とする。
  (グラフ上のマーカーは「到達可能性」の表示なので、最も近い値への
  スナップでなく最初に到達する行を採る。)

#### マーカー

- `core.Level` に `Markers []string` (json `markers,omitempty`) を追加する。
  永続化は Level の JSON にそのまま乗る。既知の値: `late_reg_end`,
  `rebuy_end`, `color_up`, `addon_break`。未知の値は保持し、検証では
  対象にしない。LevelView (表示用) には今回は出さない。

#### 構造の取得・保存 API

- `GET /api/tournaments/{id}/structure` (要認証): エディタ初期ロード用に
  {name, state, current_level, structure_version, game_mix,
  starting_stack, levels} を返す。snapshot は表示用で構造全体を
  含まないため新設する。
- 保存は commands 経路に一本化し、新コマンド `update_structure`
  {levels} を追加する。core の `Tournament.ReplaceStructure` が:
  - waiting: 全行差し替え可
  - running / paused / break: 先頭 CurrentLevel+1 行 (現レベル以前) が
    現行と完全一致すること (ロック)。一致しなければエラー。
    LevelEndsAt / RemainingSec には触らない (§8.5: 現レベルの時間は
    変更不可、時間変更は §6.1 の時間±調整)
  - finished: エラー
  - 成功時: StructureVersion を +1 し、既存の emit 経路で
    `structure_updated` イベントを配信する
- テンプレート API (要認証): GET /api/templates、
  POST /api/templates、GET /api/templates/{id}、
  PUT /api/templates/{id}、DELETE /api/templates/{id}。

#### エディタ UI

- ルート `GET /admin/editor` (要認証)。クエリ `?tournament=<id>` /
  `?template=<id>`。無指定は空の構造から始める。
  admin.html のトーナメント一覧に「構造を編集」リンクを追加する。
  既存のテキストエリア作成経路は変更しない。
- editor.js の行モデルは JS 配列。セルは input ベースで編集し、
  DOM 順でタブ移動する。行操作: 挿入 / 削除 / 複製 / ドラッグ
  並べ替え (HTML5 DnD) / 複数行選択の一括変更 (時間一括設定、削除)。
  Undo/Redo はメモリ上の履歴スタック。下書き自動保存は localStorage
  (キー = 対象種別 + id)、ロード時に下書きがあればバナーで知らせて
  復元し、保存成功で消去する。
- 列は format で可変: blind-nl は SB/BB/Ante、blind-limit は
  SB/BB/BIG BET/Ante、stud-limit は Ante/Bring-in/Complete。
  スタッド無効 (game_mix.stud_enabled = false) のトーナメントでは
  スタッドの format を候補に出さない。ゲーム列は game_mix の
  ローテーション (games の並び × levels_per_game) を初期割当し、
  行ごとに手動変更も可能とする。
- 取込/書出: ペースト取込モーダル (TSV / CSV / 空白区切りを受理。
  列は SB,BB[,Ante[,分]]、break 行は `break,分`)、CSV 取込 (ファイル)、
  CSV 書出 (ダウンロード + クリップボード)。付録 A の再現経路は
  「SB/BB の 2 列 + 取込ダイアログで一律 duration (120 分) と
  Ante = BB 同額を指定」で 47 行を再現できることとし、ハーネスで実証する。
- Break 一括挿入: 「N レベルごとに M 分」をダイアログで挿入、
  全 Break 除去ボタンも置く。カラーアップ候補位置での Break 提案は
  本 Issue では行わない (マーカー color_up は手動で付けられる)。
- 開催中ロック: structure 取得時の state が waiting 以外なら
  current_level 以下の行をグレー表示 + 入力 disabled にし、prefix を
  壊す行操作 (ロック行の削除・並べ替え・一括変更) も UI で禁止する。
  サーバー側でも ReplaceStructure が検証する二重化とする。
  受け入れ条件「開催中は現レベル以前の行がロックされる」は
  core テスト + httpapi テスト + UI のロック表示で担保する。

#### ChangeLog / 差分表示は follow-up に切る

- §8.5 の中核 (ロック、次レベル反映、version +1、structure_updated
  配信) は本 Issue で満たす。events テーブルには structure_updated が
  structure_version とスナップショット payload 付きで残る。
- §11 ChangeLog エンティティ (誰が・いつ・何を) と差分表示 UI は
  本 Issue には含めず、follow-up Issue 化を PR 本文で提案する。

#### ステップ分割

- (a) core: ladder / validate / simulate + Markers + テスト
- (b) API: analyze、structure 取得、update_structure コマンド、
  テンプレート CRUD + store
- (c) エディタ UI 基本: グリッド編集、行操作、Undo/Redo、自動保存、
  analyze 連携 (検証表示)
- (d) 取込/書出、テンプレート、ネスト、候補提示、Break 一括挿入
- (e) シミュレーション表示 + 圧迫度グラフ
- (f) 開催中ロック (UI 表示と操作禁止; サーバー検証は (b))

### 実装メモ (Step A: core の検証・シミュレーション)

2026-11-13 実装。対象は `internal/core` のみ (httpapi / store / webui は
触っていない)。

#### 変更ファイル

- `internal/core/level.go`: `Level.Markers []string` (json `markers,omitempty`)
  を追加。定数 `MarkerLateRegEnd` / `MarkerRebuyEnd` / `MarkerColorUp` /
  `MarkerAddonBreak` と補助メソッド `HasMarker`。`Level` は独自
  `UnmarshalJSON` を持つため、raw 構造体にも markers を追加して永続化で
  消えないようにした。
- `internal/core/ladder.go` (新規): マスター梯子と「綺麗な組」の判定。
- `internal/core/validate.go` (新規): `ValidateStructure` と
  `ValidationIssue` / `ValidateOptions` / `IssueSeverity`。
- `internal/core/simulate.go` (新規): `Simulate` / `Simulation` / `SimRow` と
  `NextBetSuggestions` / `BetSuggestion`。
- `internal/core/ladder_test.go`, `validate_test.go`, `simulate_test.go`
  (新規), `level_test.go` (マーカーの往復テストを追加)。
- `internal/core/level_test.go`, `internal/store/store_test.go`: `Level` に
  スライス	field が入って `==` で比較できなくなったため、構造体の比較を
  `reflect.DeepEqual` に変更 (閾値や判定は変えない)。既存の挙動は不变。

#### 決めたこと

- 梯子の仮数部: 末尾の 0 を剥がした値で判定する (1500→15、1000→1、
  1200→12、250000→25)。集合は {1, 2, 3, 4, 5, 6, 8, 12, 15, 16, 25}。
  16 (=1.6) は B2 の旧主流形で legacy 扱い。浮動小数点は一切使わず、
  割り算は整数演算と gcd による既約分数で行う。
- 正規ペア表は BB 仮数部 → SB/BB の既約分数: 1→{1/2}、12→{1/2}、
  15→{2/3}、16→{1/2}、2→{1/2}、25→{2/5, 1/2}、3→{1/2, 2/3}、
  4→{1/2}、5→{1/2, 3/5}、6→{1/2}、8→{1/2}。設計メモの「BB 仮数部 →
  許される SB 仮数部」表 (1.2→{0.6} など) を同一指数の比に翻訳した
  もので、内容は同じ。
- 提案の選び方: BB が梯子値でないときは「BB 以上の最小の梯子値」に
  移し、その BB の正規比のうち元の sb/bb に最も近い比で SB を作る。
  距離の比較は分数として正確に行い、同距離なら小さい比を優先。
  例: 700/1400 → 1000/1500、800/1500 → 1000/1500。
- `not_clean_value` (BB が梯子値でない) と `not_clean_pair` (BB は梯子値
  だが組が表に無い) は排他で出す (同じ行に二重に出さない)。
- 単調増加・2 倍超ジャンプの比較は format ファミリー (ブラインド系 /
  スタッド系) ごとに行い、Break 行は読み飛ばす。横ばいも違反として
  警告する (設計メモ「横ばい・減少」)。
- チップ単位の警告は、format が持つ強制ベットのうち設定されているもの
  (0 は未設定) について 1 フィールドにつき 1 件出す。SB 単体が
  25 で割り切れない場合はその旨をメッセージに記す。
- `format_boundary` は info。境界の比較対象は直前の **レベル** 行
  (Break は読み飛ばす)。実効リミットはブラインド系が BB、スタッド系が
  Complete。
- シミュレーションの時刻は分単位。duration は秒で持つので、累積秒を
  `toMinutes` で最も近い分へ四捨五入する (duration が整分なら厳密に一致)。
- R13: BB_end = T / 20 (整数除算)。ブラインド行のうち BB >= BB_end の最初の行を終了想定レベルとする (BB が 0 の行は除く)。
- `NextBetSuggestions`: 基準行の主値より大きい梯子値 3 個と、×1.25 / ×1.5 / ×2 を「以上の最小の梯子値」へスナップした値 (重複除去・昇順)。ブラインド行なら各候補に正規比から作った SB を付ける。Break 行は直前のレベル行を基準にする。

#### テスト結果

- `gofmt -l .` → 出力なし、`go vet ./...` → 問題なし、
  `go test ./...` → 全パッケージ PASS (core / auth / httpapi / store / webui)。
- 固定した内容:
  - 付録 A の 47 行 (Ante=BB、120 分): `ValidateStructure` の issue は 0 件。
    `Simulate(1 entry, 60000)` で L5 (BB 600) の AvgBB がちょうど 100、
    L6 (BB 800) で 100 を下回る (付録 A の R2)。
  - 付録 B: B1/B2/B3 は not_clean 系 warn なし、B4 (700/1400) と
    B6 (700/1400) は `not_clean_value`、B5 (800/1500) は
    `not_clean_pair`。いずれも提案は 1000 / 1500。
  - ルール単体: SB==BB / SB>BB / BB 減少 / 2 倍超ジャンプ / Ante>BB /
    duration 0 / BB 未設定 / 形式未知 / チップ単位 (SB 130, chip 25) /
    スタッドの順序違反 / Limit の big_bet ≠ BB×2 / 空構造 / 境界 info。
  - 梯子単体: `IsLadderValue` / `LadderValuesAround` / `CleanPair`。
  - シミュレーション: 総所要時間 (付録 A で 5640 分)、レイト締切
    (マーカー行 7 → 締切 840 分)、R13 (100 entry × 10000 → T=1,000,000、
    BB_end=50,000 → 付録 A の L24、開始 2760 分)、Break 行は AvgBB なし。
  - マーカーの JSON 往復 (行単体と PersistedState の両方で保持)。

### 実装メモ (Step B: API)

2026-11-13 実装。対象は `internal/core` (差分のみ)、`internal/httpapi`、
`internal/store`。エディタ UI (Step C 以降) は触っていない。

#### 変更ファイル

- `internal/core/level.go`: `Level.Equal(o Level) bool` を追加。`Index` は
  比較しない (行の位置は構造の変更で振り直される)、`Format` は
  `EffectiveFormat()` で比較する (空 format と blind-nl は同じ)、Markers は
  集合として比較する (nil と空スライスは同じ、順序は問わない)。
- `internal/core/tournament.go`: `ErrFinished` / `ErrStructureLocked` と
  `Tournament.ReplaceStructure(levels []Level) error` を追加。
- `internal/httpapi/analyze.go` (新規): `POST /api/structures/analyze`。
- `internal/httpapi/templates.go` (新規): テンプレートの CRUD。
- `internal/httpapi/handlers.go`: `GET /api/tournaments/{id}/structure`
  (`structureResponse` 型と `handleGetStructure`)。
- `internal/httpapi/commands.go`: `update_structure` コマンド
  (`commandRequest.Levels`、`validateStructureForSave`)。
- `internal/httpapi/server.go`: 新しいルートの登録 (すべて要認証)。
- `internal/store/store.go`: スキーマに `templates` テーブルを追加 (冪等)。
- `internal/store/templates.go` (新規): `Template` 型と CRUD。
- テスト: `internal/core/tournament_test.go` (ReplaceStructure 3 件)、
  `internal/httpapi/analyze_test.go` / `structure_test.go` /
  `templates_test.go` / `auth_test.go` (追加)、
  `internal/store/templates_test.go` (新規)。

#### 決めたこと

- `ReplaceStructure` のロック範囲は `CurrentLevel+1` 行 (現レベルを含む)。
  ロックされた行は `Level.Equal` で現行構造と比べる。`Level.Equal` は
  `Index` を比較しない: 索引は構造の変更で振り直されるためであり、
  クライアントが索引を含めない行を送ってもロック判定が壊れない。
  書換えは成功時に必ず `StructureVersion` を +1 する。
  `LevelEndsAt` / `RemainingSec` / `CurrentLevel` には触らない (spec §8.5)。
- `analyze` の issue は httpapi 側の変換型 (`analyzeIssue`) 做出的。severity
  は `core.IssueSeverity.String()` の "error" / "warn" / "info"。
  `suggest_after_index` は省略時は最終行 (core の `NextBetSuggestions` の
  規約に合わせて -1)。JSON の省略と 0 を区別するため `*int` にした。
- 絶対時刻 (*_ms) は `start_time_ms > 0` のときだけ入れる。終了予定 =
  start + TotalMin、レイト締切 = start + LateRegCloseMin (締切レベルが
  あるとき)、R13 到達 = start + EndLevelStartMin (到達する行があるとき)。
  対象が無ければ 0 のまま。
- 検証 error がある構造も `analyze` は 200 で issues を返す。400 は JSON が
  読めないときだけ。
- `update_structure` は適用前に `core.ValidateStructure` を走らせ、
  `SeverityError` の issue があればその message を 409 のエラーにする。
  warn / info は保存を妨げない (spec §8.4)。保存後のイベントは
  `structure_updated` で、既存の保存・emit の経路に乗る。
- テンプレートの行 (`Index`) は保存したものをそのまま保持する (採番は
  読み手の責務)。空の levels は API で 400 に弾くので、store には
  「空のテンプレート」が入らない。
- テンプレートの更新は `UPDATE` の後に読み直して返すので、`created_at` は
  変わらない。存在しない id は `ErrNotFound` (404)。
- 新しいルートはすべて `/api/` の下なので、セッションなしの呼び出しは
  既存の `RequireAuth` で 401 JSON になる。

#### テスト結果

- `gofmt -l .` → 出力なし、`go vet ./...` → 問題なし、
  `go test ./...` → 全パッケージ PASS (core / auth / httpapi / store / webui)。
- 固定した内容:
  - core: waiting で全差し替え (version 1)、running で prefix 変更が
    `ErrStructureLocked`・suffix 変更が成功して version +1、finished で
    `ErrFinished`。
  - analyze: 付録 B4 相当の 3 行で `not_clean_value` (warn) が row 1 に出る、
    TotalMin = 360、suggestions が空でない。壊れた構造 (duration 0) でも
    200 で `bad_duration` (error) が返る。絶対時刻は start があるときだけ。
  - structure 取得: 作成したトーナメントの levels が往復する。
  - update_structure: waiting で差し替え成功 (`structure_updated`、version
    +1)、開始後に prefix を変えると 409・suffix だけ変えると成功、
    duration 0 の構造は 409、warn だけの構造 (700/1400) は保存できる。
  - templates: POST → GET 一覧 → GET 個別 → PUT → DELETE の往復、name 空と
    levels 空は 400、存在しない id は 404。
  - store: テンプレートの往復 (levels・markers 込み)、更新で created_at が
    変わらないこと、存在しない id で ErrNotFound。

### 実装メモ (Step C: エディタ UI 基本)

2026-11-13 実装。前任セッションの未コミット部分成果 (`GET /admin/editor` の
ルート、`RenderEditor`、`editor.html`) を引き継ぎ、HTML の ID 契約を保って
クライアント UI を実装した。

#### 変更ファイル

- `internal/webui/static/editor.js`: tournament / template / 新規対象の読込、
  行グリッド・形式別ベット・マーカー・行追加/複製/削除/HTML5 DnD、ゲーム
  ローテーション、一括時間設定/削除、Undo/Redo、localStorage 下書き、
  analyze 検証表示、保存を実装。
- `internal/webui/static/admin.js`: 各トーナメントに構造編集リンクを追加。
- `internal/webui/static/style.css`: エディタのツールバー、グリッド、ベット、
  マーカー、検証 severity、ドラッグ、下書きバナー用スタイルを追加。
- `internal/webui/webui_test.go`: エディタテンプレート描画と editor.js 静的配信
  の確認を追加。
- `NOTES.md`: 本メモ。

#### 決定事項・範囲

- 編集モデルは API の level JSON 配列とし、表示上の時間を分、保存値を秒で
  換算する。セル入力はモデルに即時反映し、入力中のフォーカスを維持する。
- 新規構造は指定どおり保存を無効化し、トーナメント作成画面への保存導線は
  後続ステップに残した。
- 指定範囲外のシミュレーション表示/グラフ、候補 UI、取込/書出、テンプレート
  選択、ネスト、Break 一括挿入、開催中ロックは追加していない。
- 検証結果は Go analyze API を利用し、error issue が存在する場合は保存を
  ブロックする。テンプレートと tournament の下書きキーは kind と ID で分離。

#### 検証

- `node --check internal/webui/static/editor.js`
- `node --check internal/webui/static/admin.js`
- `gofmt -l .`
- `go vet ./...`
- `GOCACHE=/tmp/pt-go-cache go test ./...`

#### 書き手の経緯 (コーディネーター注記)

- 本ステップは当初 FCC (MiniMax M3.1 Flash) に投入したが 3 回失敗した:
  1 回目はランタイム再起動でセッション消滅 (成果なし)、2 回目は約 50 分で
  SIGTERM され部分成果 (ルート / RenderEditor / editor.html) のみ、
  3 回目は約 97 分で NIM 側のタイムアウト終了 (成果なし)。
- AGENTS.md のフォールバック規定により Codex に切り替えた。Codex は
  stdin を閉じる (`< /dev/null`) ことで正常起動し (従来の
  codex-code-mode-host 欠落は再現しなかった)、約 5 分で本ステップを完走した。
- コーディネーターが editor.js に小修正を加えた: ドラッグはハンドルからの
  開始に限定 (入力欄からの誤ドラッグ防止)、保存応答の structure_version を
  snapshot から読む、新規構造でも下書き復元を行う。

### 実装メモ (Step D: 取込/書出、テンプレート、ネスト、候補提示、Break)

- 前任 FCC セッションで作成された `structureio.js` と editor.js / editor.html の部分成果を精査して引き継ぎ、未接続だった UI 操作と静的配信確認、スタイルを補完した。
- 変更ファイル: `internal/webui/static/structureio.js`（テキスト/CSV の純粋変換関数）、`internal/webui/static/editor.js`（取込・書出、テンプレート CRUD 操作、ネスト、候補、Break 一括挿入）、`internal/webui/templates/editor.html`（各操作ボタンと共通モーダル）、`internal/webui/static/style.css`（モーダル・候補チップ等）、`internal/webui/webui_test.go`（structureio.js 配信確認）、本メモ。
- テキスト取込は 2〜4 列の SB/BB/Ante/分と break 行を受け付ける。既定時間は 20 分、2 列形式の Ante は指定時に BB と同額にする。CSV は指定ヘッダーとマーカーのセミコロン区切りで往復する。新規モードの保存操作はテンプレート作成として扱い、保存後にそのテンプレートを編集対象にする。
- Break 挿入は既存 Break をカウントせずレベル行の個数で間隔を判定する。ネストはテンプレートまたは別トーナメントの構造を取得し、指定区間を挿入する。
- 検証: Node 構文確認と指定ハーネス、gofmt、go vet、Go テストを実施（結果は Step D 完了報告に記載）。
- 書き手の経緯 (コーディネーター注記): FCC (MiniMax) は約 24 分で SIGTERM され、部分成果（structureio.js と editor.js / editor.html の途中まで）を残して終了した。エディタ JS 作業での FCC の失敗が Step C から通算 4 回目となったため、AGENTS.md のフォールバック規定により残りを Codex が完了させた。コーディネーターも独立ハーネスで付録 A 再現（47 行一致）と CSV 往復を確認済み。

### 実装メモ (Step E: シミュレーション表示 + 圧迫度グラフ)

- 変更ファイル: `internal/webui/templates/editor.html`（開始時刻、集計値、SVG パネル）、`internal/webui/static/editor.js`（analyze への時刻送信と応答描画）、`internal/webui/static/style.css`（集計カードと SVG グラフ）、本メモ。
- 開始時刻が空なら `start_time_ms: 0` とし、結果時刻は開始からの累計分で表示する。入力があればその時刻を送信し、予定時刻を HH:MM で表示する。グラフは analyze 応答内の `simulation.rows` のみから作り、平均 BB があるレベル行を折れ線・点で表示し、LR と R13 の位置をマークする。点の title にはレベル、開始時刻、BB を含める。
- 総チップ注記は想定エントリー数 × 開始スタックであり、リバイ等を含まない旨を表示する。
- 検証: `node --check` は `editor.js` / `structureio.js` / `admin.js` すべて成功。`gofmt -l .` は出力なし、`go vet ./...` は問題なし、`GOCACHE=/tmp/pt-go-cache go test ./...` は全パッケージ PASS。

### 実装メモ (Step F: 開催中ロック)

- 変更ファイル: `internal/webui/static/editor.js`（ロック範囲判定、入力・操作制御、suffix 一括操作）、`internal/webui/static/style.css`（灰色行と境界表示）、`internal/webui/templates/editor.html`（終了時刻とシミュレーションパネル）、本メモ。
- 構造の取得応答を `load()` が `base` に保持し、その `state` と `current_level`（structure endpoint の 0 始まり整数）から prefix 長を算出する。トーナメントの waiting は 0 行、running / paused / break は現在行まで、finished は全行をロックして保存を無効にする。ローカル下書き復元後もサーバー取得時の state/current_level を基準に UI 制御するため、通常の行操作、DnD、一括操作、Replace import、Nest 挿入、Break 操作、ローテーションは prefix を変更しない位置・対象に制限する。
- サーバーからの再取得やブラウザー操作による受け入れ確認は未実施。UI の制御はクライアント側での通常操作を対象とし、復元下書きが prefix を変えている場合の保存拒否は既存のサーバー 409 経路に委ねる。
- 書き手の経緯 (コーディネーター注記): Step E / F はエディタ JS の作業であり、FCC (MiniMax) は Step C / D で通算 4 回失敗していたため、AGENTS.md のフォールバック規定に基づき最初から Codex に委譲した (約 5 分で完走)。
- 検証: `node --check` は `editor.js` / `structureio.js` / `admin.js` すべて成功。`gofmt -l .` は出力なし、`go vet ./...` は問題なし、`GOCACHE=/tmp/pt-go-cache go test ./...` は全パッケージ PASS。実ブラウザー確認はコーディネーター側で未実施。
