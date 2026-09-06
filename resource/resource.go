// Package resource はリソース種別に依存しない差分計算と、CLI から扱うための
// インターフェースを提供する。scheduler や cloudtasks はこの上に実装する。
package resource

import (
	"context"
	"fmt"
	"strings"

	"github.com/sinmetalcraft/bizmac/prop"
	"gopkg.in/yaml.v3"
)

// Item は 1 件のリソース定義。yaml タグの付いた構造体へのポインタを想定する。
type Item interface {
	// ItemName はリソース ID を返す。
	ItemName() string
	// ItemIgnoreChange はリソース個別の ignore_change を返す。
	ItemIgnoreChange() []string
	// SetItemIgnoreChange は ignore_change を差し替える。
	// export で既存ファイルの設定を引き継ぐときに使う。
	SetItemIgnoreChange(paths []string)
	// Canonicalize は body/body_json のような同義表現をそろえる。
	// 差分計算の前に yaml 側・API 側の双方へ適用される。
	Canonicalize() error
	// RecreateKey は値が変わったら作り直しが必要な属性の識別子を返す。
	// (Cloud Scheduler のターゲット種別など)。概念が無いリソースは "" を返す。
	RecreateKey() string
}

// ServerDefaults は「yaml に書かなければ Google Cloud 側の値をそのまま使う」
// プロパティを持つリソースが実装する。Item の実装は任意。
// Google Cloud が既定値を必ず埋め、かつ API で未設定へ戻せないプロパティは、
// yaml に書かなかったことを差分として扱っても update で解消できないため、
// 現状の値を尊重して差分に出さない。
type ServerDefaults interface {
	ServerDefaultPaths() []string
}

// File は 1 ファイル分のリソース定義。
type File[T Item] interface {
	GetProject() string
	SetProject(project string)
	GetLocation() string
	SetLocation(location string)
	GetIgnoreChange() []string
	SetIgnoreChange(paths []string)
	GetItems() []T
	SetItems(items []T)
	// Sort はリソースを名前順に並べ替える。export の出力を安定させるために使う。
	Sort()
	// Save は設定ファイルを書き出す。path が "-" の場合は標準出力へ書く。
	Save(path string) error
}

// Service は Google Cloud 側の API クライアント。
type Service[T Item] interface {
	Close() error
	// List は指定した project/location のリソースを名前順で返す。
	List(ctx context.Context, project, location string) ([]T, error)
	Create(ctx context.Context, project, location string, item T) error
	// Update は既存リソースを更新する。changes には差分のあったプロパティが渡る。
	// フィールドマスクを差分から組み立てる実装のために使う。
	Update(ctx context.Context, project, location string, item T, changes []prop.Change) error
	Delete(ctx context.Context, project, location, name string) error
}

// Notes は直近の List で除外したリソースなどの注記を返す。
// Service の実装は任意で、満たしている場合だけ CLI が拾って表示する。
type Notes interface {
	Notes() []string
}

// MarshalItem は 1 件のリソースを yaml のリスト要素としてエンコードする。
// 出力は "- name: xxx" で始まり、以降のプロパティは 2 文字インデントされる。
func MarshalItem[T Item](item T) (string, error) {
	var sb strings.Builder
	enc := yaml.NewEncoder(&sb)
	enc.SetIndent(2)
	if err := enc.Encode([]T{item}); err != nil {
		return "", fmt.Errorf("resource: encode yaml: %w", err)
	}
	if err := enc.Close(); err != nil {
		return "", fmt.Errorf("resource: encode yaml: %w", err)
	}
	return sb.String(), nil
}
