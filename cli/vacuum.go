package cli

import (
	"fmt"

	"github.com/sinmetalcraft/bizmac/resource"
	"github.com/spf13/cobra"
)

func newVacuumCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "vacuum",
		Short: "yaml に定義の無いリソースを Google Cloud から削除する",
	}
}

func newVacuumCmdFor[T resource.Item](k kind[T]) *cobra.Command {
	var (
		flags  targetFlags
		dryRun bool
		yes    bool
	)
	cmd := &cobra.Command{
		Use:   k.name,
		Short: fmt.Sprintf("yaml に定義の無い %sを削除する", k.resourceLabel),
		Long: fmt.Sprintf("Google Cloud にあって yaml に定義が無い%sを削除する。\n", k.itemLabel) +
			"削除だけを行い、追加・更新はしない。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := buildPlan(cmd, k, &flags)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			printHeader(out, plan)

			if len(plan.Delete) == 0 {
				fmt.Fprintf(out, "削除対象の%sはありません。\n", k.itemLabel)
				return nil
			}
			for _, item := range plan.Delete {
				fmt.Fprintf(out, "- %s\n", item.ItemName())
			}
			fmt.Fprintf(out, "\n%d 件の%sが削除対象です。\n", len(plan.Delete), k.itemLabel)
			if k.vacuumWarning != "" {
				fmt.Fprintf(out, "警告: %s\n", k.vacuumWarning)
			}

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
			svc, err := k.newService(ctx)
			if err != nil {
				return err
			}
			defer svc.Close()

			for _, item := range plan.Delete {
				if err := svc.Delete(ctx, plan.Project, plan.Location, item.ItemName()); err != nil {
					return err
				}
				fmt.Fprintf(out, "deleted %s\n", item.ItemName())
			}
			return nil
		},
	}
	flags.bind(cmd, k.defaultFile)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "削除せずに対象だけを表示する")
	cmd.Flags().BoolVar(&yes, "yes", false, "確認をスキップして削除する")
	return cmd
}
