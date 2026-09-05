package cli

import (
	"context"
	"fmt"

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
