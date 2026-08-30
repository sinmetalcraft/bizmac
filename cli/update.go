package cli

import (
	"fmt"

	"github.com/sinmetalcraft/bizmac/scheduler"
	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "yaml のリソースを Google Cloud に反映する (追加・更新のみ)",
	}
	cmd.AddCommand(newUpdateSchedulerCmd())
	return cmd
}

func newUpdateSchedulerCmd() *cobra.Command {
	var (
		flags  targetFlags
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "scheduler",
		Short: "Cloud Scheduler のジョブを追加・更新する",
		Long: "yaml に定義されたジョブを Google Cloud に反映する。追加と更新だけを行い、削除はしない。\n" +
			"yaml に無いジョブを削除したい場合は vacuum を使う。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := buildSchedulerPlan(cmd, &flags)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if err := printPlan(out, plan); err != nil {
				return err
			}
			if !plan.HasChange() {
				return nil
			}
			for _, u := range plan.Update {
				if u.TargetKindChanged {
					return fmt.Errorf("ジョブ %q はターゲット種別が %s から %s へ変わっています。"+
						"update では変更できないので、一度削除して作り直してください",
						u.Name, u.Actual.TargetKind(), u.Desired.TargetKind())
				}
			}
			if dryRun {
				fmt.Fprintln(out, "\n--dry-run のため反映しませんでした。")
				return nil
			}

			ctx := cmd.Context()
			svc, err := scheduler.NewService(ctx)
			if err != nil {
				return err
			}
			defer svc.Close()

			fmt.Fprintln(out)
			for _, j := range plan.Create {
				if err := svc.Create(ctx, plan.Project, plan.Location, j); err != nil {
					return err
				}
				fmt.Fprintf(out, "created %s\n", j.Name)
			}
			for _, u := range plan.Update {
				if err := svc.Update(ctx, plan.Project, plan.Location, u.Desired); err != nil {
					return err
				}
				fmt.Fprintf(out, "updated %s\n", u.Name)
			}
			return nil
		},
	}
	flags.bind(cmd, scheduler.DefaultFileName)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "反映せずに実行内容だけを表示する")
	return cmd
}
