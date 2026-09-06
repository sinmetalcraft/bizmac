// Package cloudtasks は Cloud Tasks のキューを yaml で管理するための
// 設定ファイルの読み書きと、Google Cloud との差分計算を提供する。
// API は Queue 単位の http_target と task_ttl を扱うために v2beta3 を使う。
package cloudtasks

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultFileName は cloudtasks リソースの既定の設定ファイル名。
const DefaultFileName = "cloudtasks.yaml"

// File は cloudtasks.yaml 1 ファイル分の内容。
type File struct {
	Project string `yaml:"project,omitempty"`
	// Location はキューを配置するリージョン (例: asia-northeast1)。
	Location string `yaml:"location,omitempty"`
	// IgnoreChange は全キューに適用される、変更を無視するプロパティパス。
	IgnoreChange []string `yaml:"ignore_change,omitempty"`
	Queues       []*Queue `yaml:"queues"`
}

// Queue は Cloud Tasks のキュー 1 件。フィールドは Cloud Tasks API v2beta3 の
// Queue リソースに対応する。出力専用・別 API のフィールドは持たない
// (state は PauseQueue/ResumeQueue、purge_time と rate_limits.max_burst_size は出力専用)。
type Queue struct {
	// Name はキュー ID。フルリソース名ではなく末尾の ID だけを書く。
	Name string `yaml:"name"`
	// IgnoreChange はこのキューにだけ追加で適用される無視プロパティパス。
	IgnoreChange []string `yaml:"ignore_change,omitempty"`

	RateLimits  *RateLimits  `yaml:"rate_limits,omitempty"`
	RetryConfig *RetryConfig `yaml:"retry_config,omitempty"`
	// TaskTTL はタスクがキューに保持される最大時間。Go の duration 表記か、
	// time.Duration に収まらない場合は秒表記 (例: "315576000000.999999999s")。
	TaskTTL string `yaml:"task_ttl,omitempty"`
	// TombstoneTTL は実行・削除済みタスクの tombstone を保持する時間。
	TombstoneTTL             string                    `yaml:"tombstone_ttl,omitempty"`
	StackdriverLoggingConfig *StackdriverLoggingConfig `yaml:"stackdriver_logging_config,omitempty"`
	// AppEngineHTTPQueue は App Engine タスク向けのルーティング上書き。
	AppEngineHTTPQueue *AppEngineHTTPQueue `yaml:"app_engine_http_queue,omitempty"`
	// HTTPTarget は HTTP タスク向けの宛先上書き。
	// AppEngineHTTPQueue とは排他ではなく、両方を書ける。
	HTTPTarget *HTTPTarget `yaml:"http_target,omitempty"`
}

// RateLimits はディスパッチの流量制限。
// 0 と -1 (無制限) がどちらも意味を持つ値なので、未指定と区別するために
// 数値はポインタで持つ。書かなかったプロパティは update でも送らない。
type RateLimits struct {
	MaxDispatchesPerSecond *float64 `yaml:"max_dispatches_per_second,omitempty"`
	// MaxConcurrentDispatches は同時実行数の上限。-1 で無制限。
	MaxConcurrentDispatches *int32 `yaml:"max_concurrent_dispatches,omitempty"`
}

// RetryConfig はリトライ設定。duration 系は Go の duration 表記。
type RetryConfig struct {
	// MaxAttempts は試行回数の上限。-1 で無制限。
	MaxAttempts      *int32 `yaml:"max_attempts,omitempty"`
	MaxRetryDuration string `yaml:"max_retry_duration,omitempty"`
	MinBackoff       string `yaml:"min_backoff,omitempty"`
	MaxBackoff       string `yaml:"max_backoff,omitempty"`
	MaxDoublings     *int32 `yaml:"max_doublings,omitempty"`
}

// StackdriverLoggingConfig は Cloud Logging への書き出し設定。
type StackdriverLoggingConfig struct {
	// SamplingRatio は 0.0 から 1.0 の割合。
	SamplingRatio *float64 `yaml:"sampling_ratio,omitempty"`
}

// AppEngineHTTPQueue は App Engine タスクのルーティング上書き。
type AppEngineHTTPQueue struct {
	AppEngineRoutingOverride *AppEngineRouting `yaml:"app_engine_routing_override,omitempty"`
}

// AppEngineRouting は App Engine のルーティング指定。
type AppEngineRouting struct {
	Service  string `yaml:"service,omitempty"`
	Version  string `yaml:"version,omitempty"`
	Instance string `yaml:"instance,omitempty"`
	Host     string `yaml:"host,omitempty"`
}

// HTTPTarget は HTTP タスクの宛先と認証の上書き。
type HTTPTarget struct {
	URIOverride *URIOverride `yaml:"uri_override,omitempty"`
	HTTPMethod  string       `yaml:"http_method,omitempty"`
	// HeaderOverrides は上書きするヘッダ。API では配列だが、
	// 順序に意味が無いので yaml では map で書く。
	HeaderOverrides map[string]string `yaml:"header_overrides,omitempty"`
	OAuthToken      *OAuthToken       `yaml:"oauth_token,omitempty"`
	OIDCToken       *OIDCToken        `yaml:"oidc_token,omitempty"`
}

// URIOverride はタスクの宛先 URI の上書き。
type URIOverride struct {
	// Scheme は HTTP か HTTPS。
	Scheme string `yaml:"scheme,omitempty"`
	Host   string `yaml:"host,omitempty"`
	Port   *int64 `yaml:"port,omitempty"`
	// PathOverride は上書きするパス (例: /v1/task)。
	PathOverride string `yaml:"path_override,omitempty"`
	// QueryOverride は上書きするクエリ文字列 (例: "a=1&b=2")。
	QueryOverride string `yaml:"query_override,omitempty"`
	// URIOverrideEnforceMode は ALWAYS か IF_NOT_EXISTS。
	URIOverrideEnforceMode string `yaml:"uri_override_enforce_mode,omitempty"`
}

// OAuthToken は Google API を叩くときの OAuth トークン設定。
type OAuthToken struct {
	ServiceAccountEmail string `yaml:"service_account_email"`
	Scope               string `yaml:"scope,omitempty"`
}

// OIDCToken は Cloud Run などを叩くときの OIDC トークン設定。
type OIDCToken struct {
	ServiceAccountEmail string `yaml:"service_account_email"`
	Audience            string `yaml:"audience,omitempty"`
}

// LoadFile は設定ファイルを読み込む。ファイルが無い場合は空の File を返す。
func LoadFile(path string) (*File, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &File{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cloudtasks: read %s: %w", path, err)
	}
	var f File
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("cloudtasks: parse %s: %w", path, err)
	}
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("cloudtasks: %s: %w", path, err)
	}
	return &f, nil
}

// Validate は設定ファイルの内容を検証する。
func (f *File) Validate() error {
	seen := map[string]struct{}{}
	for i, q := range f.Queues {
		if q == nil {
			return fmt.Errorf("queues[%d] is empty", i)
		}
		if q.Name == "" {
			return fmt.Errorf("queues[%d] has no name", i)
		}
		if _, ok := seen[q.Name]; ok {
			return fmt.Errorf("queue %q is defined more than once", q.Name)
		}
		seen[q.Name] = struct{}{}
		if err := q.Validate(); err != nil {
			return fmt.Errorf("queue %q: %w", q.Name, err)
		}
	}
	return nil
}

// Validate はキュー 1 件の内容を検証する。
func (q *Queue) Validate() error {
	if t := q.HTTPTarget; t != nil {
		if t.OAuthToken != nil && t.OIDCToken != nil {
			return fmt.Errorf("http_target: oauth_token and oidc_token are mutually exclusive")
		}
	}
	return nil
}

// SortQueues はキューを名前順に並べ替える。export の出力を安定させるために使う。
func (f *File) SortQueues() {
	sort.Slice(f.Queues, func(i, k int) bool { return f.Queues[i].Name < f.Queues[k].Name })
}

// Save は設定ファイルを書き出す。path が "-" の場合は標準出力へ書く。
func (f *File) Save(path string) error {
	b, err := f.Marshal()
	if err != nil {
		return err
	}
	if path == "-" {
		_, err := os.Stdout.Write(b)
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cloudtasks: mkdir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("cloudtasks: write %s: %w", path, err)
	}
	return nil
}

// Marshal は設定ファイルを yaml へエンコードする。
func (f *File) Marshal() ([]byte, error) {
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, fmt.Errorf("cloudtasks: encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("cloudtasks: encode yaml: %w", err)
	}
	return []byte(sb.String()), nil
}

// ItemName は resource.Item の実装。
func (q *Queue) ItemName() string { return q.Name }

// ItemIgnoreChange は resource.Item の実装。
func (q *Queue) ItemIgnoreChange() []string { return q.IgnoreChange }

// SetItemIgnoreChange は resource.Item の実装。
func (q *Queue) SetItemIgnoreChange(paths []string) { q.IgnoreChange = paths }

// Canonicalize は resource.Item の実装。
// キューには body/body_json のような同義表現が無いので何もしない。
func (q *Queue) Canonicalize() error { return nil }

// RecreateKey は resource.Item の実装。
// キューには作り直しが必要な属性が無いので常に "" を返す。
func (q *Queue) RecreateKey() string { return "" }

// GetProject は resource.File の実装。
func (f *File) GetProject() string { return f.Project }

// SetProject は resource.File の実装。
func (f *File) SetProject(project string) { f.Project = project }

// GetLocation は resource.File の実装。
func (f *File) GetLocation() string { return f.Location }

// SetLocation は resource.File の実装。
func (f *File) SetLocation(location string) { f.Location = location }

// GetIgnoreChange は resource.File の実装。
func (f *File) GetIgnoreChange() []string { return f.IgnoreChange }

// SetIgnoreChange は resource.File の実装。
func (f *File) SetIgnoreChange(paths []string) { f.IgnoreChange = paths }

// GetItems は resource.File の実装。
func (f *File) GetItems() []*Queue { return f.Queues }

// SetItems は resource.File の実装。
func (f *File) SetItems(queues []*Queue) { f.Queues = queues }

// Sort は resource.File の実装。
func (f *File) Sort() { f.SortQueues() }

// serverDefaultPaths は Cloud Tasks が必ず既定値を埋めるプロパティ。
// これらは API で未設定へ戻せないので、yaml に書かなかった場合は
// Google Cloud 側の値をそのまま使い、差分にも出さない。
var serverDefaultPaths = []string{
	"rate_limits.max_dispatches_per_second",
	"rate_limits.max_concurrent_dispatches",
	"retry_config.max_attempts",
	"retry_config.max_retry_duration",
	"retry_config.min_backoff",
	"retry_config.max_backoff",
	"retry_config.max_doublings",
	"task_ttl",
	"tombstone_ttl",
}

// ServerDefaultPaths は resource.ServerDefaults の実装。
func (q *Queue) ServerDefaultPaths() []string { return serverDefaultPaths }
