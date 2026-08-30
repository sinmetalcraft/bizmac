# bizmac

Infrastructure as code for Google Cloud

Google Cloud のインフラを yaml で管理するための CLI。
現時点では Cloud Scheduler に対応している。

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

共通のフラグ:

| フラグ | 説明 |
| --- | --- |
| `-f, --file` | リソース定義の yaml ファイル。既定は `scheduler.yaml` |
| `-p, --project` | プロジェクト ID。yaml の `project` を上書きする |
| `-l, --location` | ロケーション ID。yaml の `location` を上書きする |

`export` にはさらに `-o, --output`（書き出し先。既定は `--file` と同じ）がある。

### export scheduler

指定した project / location のジョブを読んで yaml に書き出す。

```
bizmac export scheduler --project my-project --location asia-northeast1
bizmac export scheduler -f prod/scheduler.yaml   # 既存ファイルの project / location を引き継ぐ
bizmac export scheduler -o -                     # 書き出さずに標準出力で確認する
```

`--file` の既存ファイルがあれば、その `project` / `location` / `ignore_change` を引き継ぐ。
`--output` を指定すると `--file` から設定を引き継ぎつつ別の場所へ書き出せる。

### diff scheduler

```
bizmac diff scheduler
bizmac diff scheduler --exit-code    # 差分があれば exit code 1
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

### update scheduler

```
bizmac update scheduler --dry-run
bizmac update scheduler
```

実行前に diff と同じ内容を表示してから反映する。削除は行わない。

### vacuum scheduler

```
bizmac vacuum scheduler --dry-run
bizmac vacuum scheduler            # 削除対象を表示して y/N を確認する
bizmac vacuum scheduler --yes      # 確認をスキップする (CI 向け)
```

## yaml

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

### ignore_change

`ignore_change` に書いたプロパティは、差分があっても無視する。
`diff` に出さないだけでなく、`update` でも上書きしない（Google Cloud 側の値をそのまま残す）。

トップレベルに書くと全ジョブに、ジョブの中に書くとそのジョブにだけ適用される（両方書いた場合は両方が効く）。

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

Cloud Scheduler は `User-Agent` ヘッダや `attempt_deadline` などに既定値を自動で埋めるため、
最小限の yaml を書くと diff にそれらが出る。`export` した結果をベースにするか、
`ignore_change` に足しておくとよい。

## 制限

- `update` はターゲットの種別変更（`http_target` → `pubsub_target` など）には対応していない。
  この場合はエラーになるので、一度削除してから作り直す。
- 1 ファイルにつき 1 つの project / location を扱う。複数リージョンにジョブがある場合はファイルを分ける。
