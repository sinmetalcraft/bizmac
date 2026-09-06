// Package scheduler は Cloud Scheduler のジョブを yaml で管理するための
// 設定ファイルの読み書きと、Google Cloud との差分計算を提供する。
package scheduler

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultFileName は scheduler リソースの既定の設定ファイル名。
const DefaultFileName = "scheduler.yaml"

// File は scheduler.yaml 1 ファイル分の内容。
type File struct {
	Project string `yaml:"project,omitempty"`
	// Location はジョブを配置するリージョン (例: asia-northeast1)。
	Location string `yaml:"location,omitempty"`
	// IgnoreChange は全ジョブに適用される、変更を無視するプロパティパス。
	IgnoreChange []string `yaml:"ignore_change,omitempty"`
	Jobs         []*Job   `yaml:"jobs"`
}

// Job は Cloud Scheduler のジョブ 1 件。フィールドは Cloud Scheduler API の
// Job リソースに対応し、出力専用フィールド(state, status, 各種時刻)は持たない。
type Job struct {
	// Name はジョブ ID。フルリソース名ではなく末尾の ID だけを書く。
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
	// Schedule は unix-cron 形式のスケジュール (例: "0 3 * * *")。
	Schedule string `yaml:"schedule,omitempty"`
	// TimeZone は tz database の名前 (例: Asia/Tokyo)。未指定なら UTC。
	TimeZone string `yaml:"time_zone,omitempty"`
	// AttemptDeadline は Go の duration 表記 (例: "3m0s")。
	AttemptDeadline string `yaml:"attempt_deadline,omitempty"`
	// IgnoreChange はこのジョブにだけ追加で適用される無視プロパティパス。
	IgnoreChange []string `yaml:"ignore_change,omitempty"`

	RetryConfig         *RetryConfig         `yaml:"retry_config,omitempty"`
	PubsubTarget        *PubsubTarget        `yaml:"pubsub_target,omitempty"`
	AppEngineHTTPTarget *AppEngineHTTPTarget `yaml:"app_engine_http_target,omitempty"`
	HTTPTarget          *HTTPTarget          `yaml:"http_target,omitempty"`
}

// RetryConfig はリトライ設定。duration 系は Go の duration 表記。
type RetryConfig struct {
	RetryCount         int32  `yaml:"retry_count,omitempty"`
	MaxRetryDuration   string `yaml:"max_retry_duration,omitempty"`
	MinBackoffDuration string `yaml:"min_backoff_duration,omitempty"`
	MaxBackoffDuration string `yaml:"max_backoff_duration,omitempty"`
	MaxDoublings       int32  `yaml:"max_doublings,omitempty"`
}

// HTTPTarget は任意の HTTP エンドポイントを叩くターゲット。
type HTTPTarget struct {
	URI        string            `yaml:"uri"`
	HTTPMethod string            `yaml:"http_method,omitempty"`
	Headers    map[string]string `yaml:"headers,omitempty"`
	// Body はリクエストボディを生の文字列として書く場合に使う。
	Body string `yaml:"body,omitempty"`
	// BodyJSON はリクエストボディを JSON として構造化して書く場合に使う。
	// Body と BodyJSON は同時に指定できない。
	BodyJSON   any         `yaml:"body_json,omitempty"`
	OAuthToken *OAuthToken `yaml:"oauth_token,omitempty"`
	OIDCToken  *OIDCToken  `yaml:"oidc_token,omitempty"`
}

// AppEngineHTTPTarget は App Engine のハンドラを叩くターゲット。
type AppEngineHTTPTarget struct {
	HTTPMethod       string            `yaml:"http_method,omitempty"`
	AppEngineRouting *AppEngineRouting `yaml:"app_engine_routing,omitempty"`
	RelativeURI      string            `yaml:"relative_uri,omitempty"`
	Headers          map[string]string `yaml:"headers,omitempty"`
	Body             string            `yaml:"body,omitempty"`
	BodyJSON         any               `yaml:"body_json,omitempty"`
}

// AppEngineRouting は App Engine のルーティング指定。
type AppEngineRouting struct {
	Service  string `yaml:"service,omitempty"`
	Version  string `yaml:"version,omitempty"`
	Instance string `yaml:"instance,omitempty"`
	Host     string `yaml:"host,omitempty"`
}

// PubsubTarget は Pub/Sub トピックへ publish するターゲット。
type PubsubTarget struct {
	// TopicName は projects/PROJECT_ID/topics/TOPIC_ID 形式。
	TopicName string `yaml:"topic_name"`
	// Data はメッセージ本文を生の文字列として書く場合に使う。
	Data string `yaml:"data,omitempty"`
	// DataJSON はメッセージ本文を JSON として構造化して書く場合に使う。
	DataJSON   any               `yaml:"data_json,omitempty"`
	Attributes map[string]string `yaml:"attributes,omitempty"`
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
		return nil, fmt.Errorf("scheduler: read %s: %w", path, err)
	}
	var f File
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("scheduler: parse %s: %w", path, err)
	}
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("scheduler: %s: %w", path, err)
	}
	return &f, nil
}

// Validate は設定ファイルの内容を検証する。
func (f *File) Validate() error {
	seen := map[string]struct{}{}
	for i, j := range f.Jobs {
		if j == nil {
			return fmt.Errorf("jobs[%d] is empty", i)
		}
		if j.Name == "" {
			return fmt.Errorf("jobs[%d] has no name", i)
		}
		if _, ok := seen[j.Name]; ok {
			return fmt.Errorf("job %q is defined more than once", j.Name)
		}
		seen[j.Name] = struct{}{}
		if err := j.Validate(); err != nil {
			return fmt.Errorf("job %q: %w", j.Name, err)
		}
	}
	return nil
}

// Validate はジョブ 1 件の内容を検証する。
func (j *Job) Validate() error {
	targets := 0
	if j.HTTPTarget != nil {
		targets++
		if j.HTTPTarget.Body != "" && j.HTTPTarget.BodyJSON != nil {
			return fmt.Errorf("http_target: body and body_json are mutually exclusive")
		}
	}
	if j.AppEngineHTTPTarget != nil {
		targets++
		if j.AppEngineHTTPTarget.Body != "" && j.AppEngineHTTPTarget.BodyJSON != nil {
			return fmt.Errorf("app_engine_http_target: body and body_json are mutually exclusive")
		}
	}
	if j.PubsubTarget != nil {
		targets++
		if j.PubsubTarget.Data != "" && j.PubsubTarget.DataJSON != nil {
			return fmt.Errorf("pubsub_target: data and data_json are mutually exclusive")
		}
	}
	switch targets {
	case 1:
		return nil
	case 0:
		return fmt.Errorf("one of http_target, app_engine_http_target, pubsub_target is required")
	default:
		return fmt.Errorf("only one target can be set, but %d are set", targets)
	}
}

// SortJobs はジョブを名前順に並べ替える。export の出力を安定させるために使う。
func (f *File) SortJobs() {
	sort.Slice(f.Jobs, func(i, k int) bool { return f.Jobs[i].Name < f.Jobs[k].Name })
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
			return fmt.Errorf("scheduler: mkdir %s: %w", dir, err)
		}
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		return fmt.Errorf("scheduler: write %s: %w", path, err)
	}
	return nil
}

// Marshal は設定ファイルを yaml へエンコードする。
func (f *File) Marshal() ([]byte, error) {
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, fmt.Errorf("scheduler: encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("scheduler: encode yaml: %w", err)
	}
	return []byte(sb.String()), nil
}

// encodePayload は body / body_json のペアを実際に送信するバイト列へ変換する。
func encodePayload(raw string, structured any) ([]byte, error) {
	if structured != nil {
		b, err := json.Marshal(structured)
		if err != nil {
			return nil, fmt.Errorf("encode json payload: %w", err)
		}
		return b, nil
	}
	if raw == "" {
		return nil, nil
	}
	return []byte(raw), nil
}

// decodePayload はバイト列を body / body_json のペアへ戻す。
// JSON のオブジェクト・配列としてパースできる場合は構造化した側に入れる。
func decodePayload(b []byte) (raw string, structured any) {
	if len(b) == 0 {
		return "", nil
	}
	trimmed := strings.TrimSpace(string(b))
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var v any
		if err := json.Unmarshal(b, &v); err == nil {
			return "", v
		}
	}
	return string(b), nil
}

// canonicalizePayloads は body / body_json をどちらで書いても同じ差分になるよう、
// 表現を一意な形へそろえる。diff の前に yaml 側・API 側の双方へ適用する。
func (j *Job) canonicalizePayloads() error {
	if t := j.HTTPTarget; t != nil {
		b, err := encodePayload(t.Body, t.BodyJSON)
		if err != nil {
			return err
		}
		t.Body, t.BodyJSON = decodePayload(b)
	}
	if t := j.AppEngineHTTPTarget; t != nil {
		b, err := encodePayload(t.Body, t.BodyJSON)
		if err != nil {
			return err
		}
		t.Body, t.BodyJSON = decodePayload(b)
	}
	if t := j.PubsubTarget; t != nil {
		b, err := encodePayload(t.Data, t.DataJSON)
		if err != nil {
			return err
		}
		t.Data, t.DataJSON = decodePayload(b)
	}
	return nil
}

// TargetKind はターゲットの種別名を返す。update での種別変更検出に使う。
func (j *Job) TargetKind() string {
	switch {
	case j.HTTPTarget != nil:
		return "http_target"
	case j.AppEngineHTTPTarget != nil:
		return "app_engine_http_target"
	case j.PubsubTarget != nil:
		return "pubsub_target"
	default:
		return ""
	}
}

// ItemName は resource.Item の実装。
func (j *Job) ItemName() string { return j.Name }

// ItemIgnoreChange は resource.Item の実装。
func (j *Job) ItemIgnoreChange() []string { return j.IgnoreChange }

// SetItemIgnoreChange は resource.Item の実装。
func (j *Job) SetItemIgnoreChange(paths []string) { j.IgnoreChange = paths }

// Canonicalize は resource.Item の実装。
func (j *Job) Canonicalize() error { return j.canonicalizePayloads() }

// RecreateKey は resource.Item の実装。ジョブはターゲット種別を変えられない。
func (j *Job) RecreateKey() string { return j.TargetKind() }

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
func (f *File) GetItems() []*Job { return f.Jobs }

// SetItems は resource.File の実装。
func (f *File) SetItems(jobs []*Job) { f.Jobs = jobs }

// Sort は resource.File の実装。
func (f *File) Sort() { f.SortJobs() }
