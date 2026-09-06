package cli

import (
	"fmt"

	"github.com/sinmetalcraft/bizmac/resource"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "diff",
		Short: "yaml と Google Cloud の現在のリソースの差分を表示する",
	}
}

func newDiffCmdFor[T resource.Item](k kind[T]) *cobra.Command {
	var (
		flags    targetFlags
		exitCode bool
		against  string
	)
	cmd := &cobra.Command{
		Use:   k.name,
		Short: fmt.Sprintf("%sの差分を表示する", k.resourceLabel),
		Args:  cobra.NoArgs,
		Long: fmt.Sprintf("yaml に定義された%sと Google Cloud の現状を比較して差分を表示する。\n", k.itemLabel) +
			"--against に別の yaml を指定した場合は Google Cloud には接続せず、\n" +
			"--file をあるべき姿、--against を現状としてファイル同士を比較する。",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if against != "" {
				fp, err := buildFilePlan(k, flags.file, against)
				if err != nil {
					return err
				}
				if err := printFilePlan(cmd.OutOrStdout(), flags.file, against, fp); err != nil {
					return err
				}
				if exitCode && (fp.plan.HasChange() || len(fp.plan.Delete) > 0) {
					return &exitError{code: 1}
				}
				return nil
			}

			plan, err := buildPlan(cmd, k, &flags)
			if err != nil {
				return err
			}
			if err := printPlan(cmd.OutOrStdout(), plan); err != nil {
				return err
			}
			if exitCode && (plan.HasChange() || len(plan.Delete) > 0) {
				// CI で差分の有無を判定できるようにする。
				return &exitError{code: 1}
			}
			return nil
		},
	}
	flags.bind(cmd, k.defaultFile)
	cmd.Flags().BoolVar(&exitCode, "exit-code", false, "差分がある場合に exit code 1 で終了する")
	cmd.Flags().StringVar(&against, "against", "",
		"比較対象の yaml ファイル。指定すると Google Cloud ではなくファイル同士を比較する")
	return cmd
}
