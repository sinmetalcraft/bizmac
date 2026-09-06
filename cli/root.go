// Package cli は bizmac のコマンドライン実装。
package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
)

// Execute はルートコマンドを実行する。
func Execute() error {
	return NewRootCmd().Execute()
}

// NewRootCmd は bizmac のルートコマンドを組み立てる。
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "bizmac",
		Short:         "Infrastructure as code for Google Cloud",
		Long:          "bizmac は Google Cloud のインフラを yaml で管理するためのツール。",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	export, diff, update, vacuum := newExportCmd(), newDiffCmd(), newUpdateCmd(), newVacuumCmd()
	// リソース種別を増やすときは kinds に 1 つ足す。
	kinds := []cmdSet{
		buildCmds(schedulerKind()),
		buildCmds(cloudtasksKind()),
	}
	for _, k := range kinds {
		export.AddCommand(k.export)
		diff.AddCommand(k.diff)
		update.AddCommand(k.update)
		vacuum.AddCommand(k.vacuum)
	}
	root.AddCommand(export, diff, update, vacuum)
	return root
}

// targetFlags は各サブコマンドに共通のフラグ。
type targetFlags struct {
	file     string
	project  string
	location string
}

func (f *targetFlags) bind(cmd *cobra.Command, defaultFile string) {
	cmd.Flags().StringVarP(&f.file, "file", "f", defaultFile, "リソース定義の yaml ファイル")
	cmd.Flags().StringVarP(&f.project, "project", "p", "", "Google Cloud のプロジェクト ID (yaml の project を上書きする)")
	cmd.Flags().StringVarP(&f.location, "location", "l", "", "ロケーション ID (yaml の location を上書きする)")
}

// confirm は y/N の確認を取る。EOF や y 以外の入力は false を返す。
func confirm(in io.Reader, out io.Writer, msg string) (bool, error) {
	fmt.Fprintf(out, "%s [y/N]: ", msg)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return false, fmt.Errorf("読み取りに失敗しました: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}
