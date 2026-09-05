# bizmac

Infrastructure as code for Google Cloud

Google Cloud のインフラを yaml で管理するための CLI。
現時点では Cloud Scheduler と Cloud Tasks に対応している。

## 名前の由来

Bizmac（Bismuth［ビスマス］+ IaC）

幾何学的で美しい結晶構造を作るビスマス。コードによって整然と構築されるインフラ基盤に重なります。

## インストール

```
go install github.com/sinmetalcraft/bizmac@latest
```

## 認証

Application Default Credentials を使う。

```
gcloud auth application-default login
```

## コマンド

いずれのコマンドも `bizmac <command> <resource>` の形で、`<resource>` に対象のリソース種別が入る。

| コマンド | 説明 |
| --- | --- |
| `export` | Google Cloud の現在のリソースを yaml に書き出す |
| `diff` | yaml と Google Cloud の現在のリソースの差分を表示する |
| `update` | yaml のリソースを Google Cloud に反映する。追加と更新のみで、削除はしない |
| `vacuum` | Google Cloud にあって yaml に無いリソースを削除する |

| リソース | 対象 | 既定のファイル |
| --- | --- | --- |
| `scheduler` | Cloud Scheduler のジョブ | `scheduler.yaml` |
| `cloudtasks` | Cloud Tasks のキュー | `cloudtasks.yaml` |

共通のフラグ:

| フラグ | 説明 |
| --- | --- |
| `-f, --file` | リソース定義の yaml ファイル。既定はリソースごとのファイル名 |
| `-p, --project` | プロジェクト ID。yaml の `project` を上書きする |
| `-l, --location` | ロケーション ID。yaml の `location` を上書きする |

### export

指定した project / location のリソースを読んで yaml に書き出す。

```
bizmac export scheduler --project my-project --location asia-northeast1
bizmac export cloudtasks -f prod/cloudtasks.yaml   # 既存ファイルの project / location を引き継ぐ
bizmac export scheduler -o -                       # 書き出さずに標準出力で確認する
```

`--file` の既存ファイルがあれば、その `project` / `location` / `ignore_change` を引き継ぐ。
`--output`（`-o`）を指定すると `--file` から設定を引き継ぎつつ別の場所へ書き出せる。

### diff

```
bizmac diff scheduler
bizmac diff cloudtasks --exit-code    # 差分があれば exit code 1
```

出力例:

```
project:  my-project
location: asia-northeast1

+ create weekly-report
    - name: weekly-report
      schedule: 0 9 * * 1
      http_target:
        uri: https://report.example.com/weekly
        http_method: POST
~ update nightly-batch
    ~ schedule: "0 3 * * *" => "0 4 * * *"
    + retry_config.retry_count: 3
- vacuum old-job (yaml に定義がありません)

create: 1, update: 1, no change: 2, vacuum candidate: 1
```

### update

```
bizmac update scheduler --dry-run
bizmac update cloudtasks
```

実行前に diff と同じ内容を表示してから反映する。削除は行わない。

### vacuum

```
bizmac vacuum scheduler --dry-run
bizmac vacuum cloudtasks            # 削除対象を表示して y/N を確認する
bizmac vacuum scheduler --yes       # 確認をスキップする (CI 向け)
```

### ignore_change

`ignore_change` に書いたプロパティは、差分があっても無視する。
`diff` に出さないだけでなく、`update` でも上書きしない（Google Cloud 側の値をそのまま残す）。

トップレベルに書くと全リソースに、リソースの中に書くとそのリソースにだけ適用される（両方書いた場合は両方が効く）。

```yaml
ignore_change:
  - description                          # 全ジョブの description を無視

jobs:
  - name: nightly-batch
    ignore_change:
      - http_target.headers.User-Agent   # このジョブのこのヘッダだけ無視
      - retry_config.max_doublings
```

パスはドット区切りで、map のキーもそのまま書ける。

Google Cloud はプロパティに既定値を自動で埋めるため、最小限の yaml を書くと diff にそれらが出る。
`export` した結果をベースにするか、`ignore_change` に足しておくとよい。

## Cloud Scheduler (`scheduler`)

`_example/scheduler.yaml` に一通りの例がある。

```yaml
project: my-project
location: asia-northeast1

ignore_change:
  - http_target.headers.User-Agent

jobs:
  - name: nightly-batch
    description: 毎晩のバッチ処理
    schedule: "0 3 * * *"
    time_zone: Asia/Tokyo
    attempt_deadline: 3m0s
    retry_config:
      retry_count: 3
      min_backoff_duration: 5s
    http_target:
      uri: https://batch.example.com/run
      http_method: POST
      headers:
        Content-Type: application/json
      body_json:
        kind: batch
        target: users
      oidc_token:
        service_account_email: scheduler@my-project.iam.gserviceaccount.com
```

### ジョブのプロパティ

Cloud Scheduler API の [Job](https://cloud.google.com/scheduler/docs/reference/rest/v1/projects.locations.jobs) リソースに対応する。
`state` や `status` などの出力専用フィールドは扱わない。

- `name` — ジョブ ID。フルリソース名ではなく末尾の ID だけを書く
- `description`
- `schedule` — unix-cron 形式
- `time_zone` — tz database の名前。未指定なら UTC
- `attempt_deadline` — Go の duration 表記 (`3m0s`, `180s` など)
- `retry_config` — `retry_count` / `max_retry_duration` / `min_backoff_duration` / `max_backoff_duration` / `max_doublings`。duration 系は Go の duration 表記
- ターゲットは `http_target` / `app_engine_http_target` / `pubsub_target` のいずれか 1 つ

### body の書き方

`http_target` の body は、生の文字列で書く `body` と、JSON として構造化して書く `body_json` の
どちらでも指定できる。両方を同時に指定することはできない。

```yaml
# 生の文字列で書く
http_target:
  body: |
    {"kind": "batch", "target": "users"}

# JSON として構造化して書く
http_target:
  body_json:
    kind: batch
    target: users
```

どちらで書いても diff の結果は同じになる。`pubsub_target` も同様に `data` / `data_json` が使える。
`export` は JSON としてパースできる body を `body_json` で書き出す。

### 制限

- `update` はターゲットの種別変更（`http_target` → `pubsub_target` など）には対応していない。
  この場合はエラーになるので、一度削除してから作り直す。

## Cloud Tasks (`cloudtasks`)

`_example/cloudtasks.yaml` に一通りの例がある。

```yaml
project: my-project
location: asia-northeast1

queues:
  - name: process-nft
    rate_limits:
      max_dispatches_per_second: 500
      max_concurrent_dispatches: 1
    retry_config:
      max_attempts: -1 # -1 で無制限
      min_backoff: 100ms
      max_backoff: 1h0m0s
      max_doublings: 16
    stackdriver_logging_config:
      sampling_ratio: 1

  - name: to-cloud-run
    http_target:
      uri_override:
        scheme: HTTPS
        host: task-handler-xxxxxxxxxx-an.a.run.app
        path_override: /v1/task
      header_overrides:
        X-Origin: bizmac
      oidc_token:
        service_account_email: tasks@my-project.iam.gserviceaccount.com

  - name: backup-datastore
    app_engine_http_queue:
      app_engine_routing_override:
        host: ah-builtin-python-bundle.my-project.appspot.com
```

### キューのプロパティ

Cloud Tasks API v2beta3 の [Queue](https://cloud.google.com/tasks/docs/reference/rest/v2beta3/projects.locations.queues) リソースに対応する。
管理するのはキューだけで、タスクは扱わない。

- `name` — キュー ID。フルリソース名ではなく末尾の ID だけを書く
- `rate_limits` — `max_dispatches_per_second` / `max_concurrent_dispatches`
- `retry_config` — `max_attempts` / `max_retry_duration` / `min_backoff` / `max_backoff` / `max_doublings`
- `task_ttl` / `tombstone_ttl`
- `stackdriver_logging_config` — `sampling_ratio`
- `app_engine_http_queue.app_engine_routing_override` — App Engine タスクのルーティング上書き
- `http_target` — HTTP タスクの宛先上書き。`uri_override` / `http_method` / `header_overrides` / `oauth_token` / `oidc_token`

`app_engine_http_queue` と `http_target` は排他ではない。前者は App Engine タスク、
後者は HTTP タスクに効くので、必要なら両方書ける。

`header_overrides` は API では配列だが、順序に意味が無いので yaml では map で書く。

### 書かなかったプロパティの扱い

Cloud Tasks は `rate_limits` / `retry_config` / `task_ttl` / `tombstone_ttl` に必ず既定値を埋め、
これらを API で「未設定」へ戻すことはできない。そのため bizmac はこれらのプロパティについて、
**yaml に書かなければ Google Cloud 側の現在の値をそのまま使う**（差分に出さず、`update` でも触らない）。

書けばそのとおりに反映されるので、管理したいプロパティだけを書けばよい。

```yaml
queues:
  # max_dispatches_per_second だけを管理する。
  # max_concurrent_dispatches や retry_config は Google Cloud 側の値のまま。
  - name: process-nft
    rate_limits:
      max_dispatches_per_second: 500
```

一方 `http_target` / `app_engine_http_queue` / `stackdriver_logging_config` は API で消せるので、
yaml から外すと差分に出て `update` で削除される。

### 数値プロパティと duration

`max_attempts` や `max_concurrent_dispatches` は `-1` が「無制限」、`0` も意味のある値なので、
**プロパティを書かないことと `0` を書くことは区別される**。`update` は差分のあったプロパティだけを
送るので、yaml に書いていないプロパティが `0` で上書きされることはない。

duration は Go の duration 表記（`100ms`, `1h0m0s`）で書く。
`queue.yaml` から移行したキューは `task_ttl` に約 1 万年（`315576000000.999999999s`）が入っており、
これは Go の `time.Duration` に収まらないので秒表記のまま扱う。`export` もその形で書き出す。

### 扱わないプロパティ

- `state` (`RUNNING` / `PAUSED`) — `UpdateQueue` では変更できない。一時停止・再開は運用操作として
  `gcloud tasks queues pause/resume` で行う。bizmac はこの値を読まないので、手で止めたキューを
  `update` が勝手に再開することもない
- `purge_time` / `rate_limits.max_burst_size` — 出力専用
- `type` — 下記のとおり PULL キューは管理対象外

### 制限

- beta API (v2beta3) を使っている。キュー単位の `http_target` と `task_ttl` / `tombstone_ttl` が
  GA (v2) に無いため。beta なので破壊的変更のリスクはある
- PULL キューは管理対象外。App Engine 時代の遺物で新規作成もできないため、`export` にも出さず、
  `vacuum` の削除候補にもしない。除外したキューは `note:` として表示する
- キューを削除すると中のタスクは失われ、**同じ名前のキューを 7 日間作り直せない**

## 制限

- 1 ファイルにつき 1 つの project / location を扱う。複数リージョンにリソースがある場合はファイルを分ける。
