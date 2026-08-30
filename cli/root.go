// Package cli は bizmac のコマンドライン実装。
package cli

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/sinmetalcraft/bizmac/scheduler"
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
	root.AddCommand(
		newExportCmd(),
		newDiffCmd(),
		newUpdateCmd(),
		newVacuumCmd(),
	)
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

// loadSchedulerFile は yaml を読み、フラグで project / location を上書きして返す。
func (f *targetFlags) loadSchedulerFile() (*scheduler.File, error) {
	file, err := scheduler.LoadFile(f.file)
	if err != nil {
		return nil, err
	}
	if f.project != "" {
		file.Project = f.project
	}
	if f.location != "" {
		file.Location = f.location
	}
	if file.Project == "" {
		return nil, fmt.Errorf("project が指定されていません。%s に project を書くか --project を指定してください", f.file)
	}
	if file.Location == "" {
		return nil, fmt.Errorf("location が指定されていません。%s に location を書くか --location を指定してください", f.file)
	}
	return file, nil
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
