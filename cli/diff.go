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
	)
	cmd := &cobra.Command{
		Use:   k.name,
		Short: fmt.Sprintf("%sの差分を表示する", k.resourceLabel),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
	return cmd
}
