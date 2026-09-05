package cli

import (
	"fmt"

	"github.com/sinmetalcraft/bizmac/resource"
	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "yaml のリソースを Google Cloud に反映する (追加・更新のみ)",
	}
}

func newUpdateCmdFor[T resource.Item](k kind[T]) *cobra.Command {
	var (
		flags  targetFlags
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   k.name,
		Short: fmt.Sprintf("%sを追加・更新する", k.resourceLabel),
		Long: fmt.Sprintf("yaml に定義された%sを Google Cloud に反映する。追加と更新だけを行い、削除はしない。\n", k.itemLabel) +
			fmt.Sprintf("yaml に無い%sを削除したい場合は vacuum を使う。", k.itemLabel),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			plan, err := buildPlan(cmd, k, &flags)
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
				if u.RecreateRequired {
					return fmt.Errorf("%s %q は種別が %s から %s へ変わっています。"+
						"update では変更できないので、一度削除して作り直してください",
						k.itemLabel, u.Name, u.Actual.RecreateKey(), u.Desired.RecreateKey())
				}
			}
			if dryRun {
				fmt.Fprintln(out, "\n--dry-run のため反映しませんでした。")
				return nil
			}

			ctx := cmd.Context()
			svc, err := k.newService(ctx)
			if err != nil {
				return err
			}
			defer svc.Close()

			fmt.Fprintln(out)
			for _, item := range plan.Create {
				if err := svc.Create(ctx, plan.Project, plan.Location, item); err != nil {
					return err
				}
				fmt.Fprintf(out, "created %s\n", item.ItemName())
			}
			for _, u := range plan.Update {
				if err := svc.Update(ctx, plan.Project, plan.Location, u.Desired, u.Changes); err != nil {
					return err
				}
				fmt.Fprintf(out, "updated %s\n", u.Name)
			}
			return nil
		},
	}
	flags.bind(cmd, k.defaultFile)
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "反映せずに実行内容だけを表示する")
	return cmd
}
