package cli

import (
	"fmt"

	"github.com/sinmetalcraft/bizmac/scheduler"
	"github.com/spf13/cobra"
)

func newVacuumCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vacuum",
		Short: "yaml に定義の無いリソースを Google Cloud から削除する",
	}
	cmd.AddCommand(newVacuumSchedulerCmd())
	return cmd
}

func newVacuumSchedulerCmd() *cobra.Command {
	var (
		flags  targetFlags
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   "scheduler",
		Short: "yaml に定義の無い Cloud Scheduler のジョブを削除する",
		Long: "Google Cloud にあって yaml に定義が無いジョブを削除する。\n" +
			"削除だけを行い、追加・更新はしない。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := buildSchedulerPlan(cmd, &flags)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "project:  %s\n", plan.Project)
			fmt.Fprintf(out, "location: %s\n\n", plan.Location)

			if len(plan.Delete) == 0 {
				fmt.Fprintln(out, "削除対象のジョブはありません。")
				return nil
			}
			for _, j := range plan.Delete {
				fmt.Fprintf(out, "- %s\n", j.Name)
			}
			fmt.Fprintf(out, "\n%d 件のジョブが削除対象です。\n", len(plan.Delete))

			if dryRun {
				fmt.Fprintln(out, "--dry-run のため削除しませんでした。")
				return nil
			}
			if !yes {
				ok, err := confirm(cmd.InOrStdin(), out, "本当に削除しますか?")
				if err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(out, "中止しました。")
					return nil
				}
			}

			ctx := cmd.Context()
			svc, err := scheduler.NewService(ctx)
			if err != nil {
				return err
			}
			defer svc.Close()

			for _, j := range plan.Delete {
				if err := svc.Delete(ctx, plan.Project, plan.Location, j.Name); err != nil {
					return err
				}
				fmt.Fprintf(out, "deleted %s\n", j.Name)
			}
			return nil
		},
	}
	flags.bind(cmd, scheduler.DefaultFileName)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "削除せずに対象だけを表示する")
	cmd.Flags().BoolVar(&yes, "yes", false, "確認をスキップして削除する")
	return cmd
}
