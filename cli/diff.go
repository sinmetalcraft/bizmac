package cli

import (
	"github.com/sinmetalcraft/bizmac/scheduler"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "yaml と Google Cloud の現在のリソースの差分を表示する",
	}
	cmd.AddCommand(newDiffSchedulerCmd())
	return cmd
}

func newDiffSchedulerCmd() *cobra.Command {
	var (
		flags    targetFlags
		exitCode bool
	)
	cmd := &cobra.Command{
		Use:   "scheduler",
		Short: "Cloud Scheduler のジョブの差分を表示する",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := buildSchedulerPlan(cmd, &flags)
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
	flags.bind(cmd, scheduler.DefaultFileName)
	cmd.Flags().BoolVar(&exitCode, "exit-code", false, "差分がある場合に exit code 1 で終了する")
	return cmd
}

// buildSchedulerPlan は yaml を読み、Google Cloud の現状と突き合わせて Plan を作る。
func buildSchedulerPlan(cmd *cobra.Command, flags *targetFlags) (*scheduler.Plan, error) {
	file, err := flags.loadSchedulerFile()
	if err != nil {
		return nil, err
	}

	ctx := cmd.Context()
	svc, err := scheduler.NewService(ctx)
	if err != nil {
		return nil, err
	}
	defer svc.Close()

	actual, err := svc.List(ctx, file.Project, file.Location)
	if err != nil {
		return nil, err
	}
	return scheduler.BuildPlan(file, actual)
}
