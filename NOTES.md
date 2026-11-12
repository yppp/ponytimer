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
