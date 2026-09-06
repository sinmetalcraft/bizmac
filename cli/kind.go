package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/sinmetalcraft/bizmac/resource"
	"github.com/spf13/cobra"
)

// kind は 1 つのリソース種別を CLI から扱うための情報。
// リソースを増やすときはこれを 1 つ書いて root に登録する。
type kind[T resource.Item] struct {
	// name はサブコマンド名 (例: "scheduler")。
	name string
	// itemLabel は 1 件のリソースの呼び名 (例: "ジョブ")。
	itemLabel string
	// resourceLabel はヘルプに出す説明 (例: "Cloud Scheduler のジョブ")。
	resourceLabel string
	// defaultFile は --file の既定値。
	defaultFile string
	// newFile は空の設定ファイルを作る。export の書き出しに使う。
	newFile func() resource.File[T]
	// loadFile は設定ファイルを読む。ファイルが無い場合は空の File を返す。
	loadFile func(path string) (resource.File[T], error)
	// newItem は空のリソースを作る。差分計算で使う。
	newItem func() T
	// newService は Google Cloud の API クライアントを作る。
	newService func(ctx context.Context) (resource.Service[T], error)
	// vacuumWarning は削除の確認前に出す、リソース固有の警告。
	vacuumWarning string
}

// cmdSet は 1 リソース分のサブコマンド。型パラメータを外して root に渡すために使う。
type cmdSet struct {
	export *cobra.Command
	diff   *cobra.Command
	update *cobra.Command
	vacuum *cobra.Command
}

// buildCmds は kind から 4 つのサブコマンドを組み立てる。
func buildCmds[T resource.Item](k kind[T]) cmdSet {
	return cmdSet{
		export: newExportCmdFor(k),
		diff:   newDiffCmdFor(k),
		update: newUpdateCmdFor(k),
		vacuum: newVacuumCmdFor(k),
	}
}

// loadFile は yaml を読み、フラグで project / location を上書きして返す。
func loadFile[T resource.Item](k kind[T], flags *targetFlags) (resource.File[T], error) {
	file, err := k.loadFile(flags.file)
	if err != nil {
		return nil, err
	}
	if flags.project != "" {
		file.SetProject(flags.project)
	}
	if flags.location != "" {
		file.SetLocation(flags.location)
	}
	if file.GetProject() == "" {
		return nil, fmt.Errorf("project が指定されていません。%s に project を書くか --project を指定してください", flags.file)
	}
	if file.GetLocation() == "" {
		return nil, fmt.Errorf("location が指定されていません。%s に location を書くか --location を指定してください", flags.file)
	}
	return file, nil
}

// buildPlan は yaml を読み、Google Cloud の現状と突き合わせて Plan を作る。
func buildPlan[T resource.Item](cmd *cobra.Command, k kind[T], flags *targetFlags) (*resource.Plan[T], error) {
	file, err := loadFile(k, flags)
	if err != nil {
		return nil, err
	}

	ctx := cmd.Context()
	svc, err := k.newService(ctx)
	if err != nil {
		return nil, err
	}
	defer svc.Close()

	actual, err := svc.List(ctx, file.GetProject(), file.GetLocation())
	if err != nil {
		return nil, err
	}
	plan, err := resource.BuildPlan(file.GetProject(), file.GetLocation(),
		file.GetIgnoreChange(), file.GetItems(), actual, k.newItem)
	if err != nil {
		return nil, err
	}
	// List で除外したリソースがあれば注記として持ち回る。
	if n, ok := svc.(resource.Notes); ok {
		plan.Notes = n.Notes()
	}
	return plan, nil
}

// filePlan は 2 つの yaml を比較した結果。
type filePlan[T resource.Item] struct {
	plan *resource.Plan[T]
	// file は --file 側 (あるべき姿として扱う)。
	file resource.File[T]
	// against は --against 側 (現状として扱う)。
	against resource.File[T]
}

// buildFilePlan は 2 つの yaml を突き合わせて Plan を作る。Google Cloud には接続しない。
// project / location はファイルごとに違って当然なので比較しない。
func buildFilePlan[T resource.Item](k kind[T], filePath, againstPath string) (*filePlan[T], error) {
	// 片方が読めないと「全部そちらに無い」という差分になってしまうので、
	// ファイル比較のときはパスの間違いをエラーにする。
	for _, p := range []string{filePath, againstPath} {
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("%s が読めません: %w", p, err)
		}
	}

	file, err := k.loadFile(filePath)
	if err != nil {
		return nil, err
	}
	against, err := k.loadFile(againstPath)
	if err != nil {
		return nil, err
	}
	file.Sort()
	against.Sort()

	// ignore_change は両方のファイルに書ける。dev と prod で意図的に変えている
	// プロパティを黙らせたいので、両方の和集合を適用する。
	ignore := make([]string, 0, len(file.GetIgnoreChange())+len(against.GetIgnoreChange()))
	ignore = append(ignore, file.GetIgnoreChange()...)
	ignore = append(ignore, against.GetIgnoreChange()...)

	againstIgnore := make(map[string][]string, len(against.GetItems()))
	for _, item := range against.GetItems() {
		if len(item.ItemIgnoreChange()) > 0 {
			againstIgnore[item.ItemName()] = item.ItemIgnoreChange()
		}
		// against 側の ignore_change は比較対象のプロパティではないので落とす。
		item.SetItemIgnoreChange(nil)
	}
	for _, item := range file.GetItems() {
		extra, ok := againstIgnore[item.ItemName()]
		if !ok {
			continue
		}
		merged := make([]string, 0, len(item.ItemIgnoreChange())+len(extra))
		merged = append(merged, item.ItemIgnoreChange()...)
		merged = append(merged, extra...)
		item.SetItemIgnoreChange(merged)
	}

	plan, err := resource.BuildPlan(file.GetProject(), file.GetLocation(),
		ignore, file.GetItems(), against.GetItems(), k.newItem)
	if err != nil {
		return nil, err
	}
	return &filePlan[T]{plan: plan, file: file, against: against}, nil
}
