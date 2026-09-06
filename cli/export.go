package cli

import (
	"fmt"

	"github.com/sinmetalcraft/bizmac/resource"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "export",
		Short: "Google Cloud の現在のリソースを yaml に書き出す",
	}
}

func newExportCmdFor[T resource.Item](k kind[T]) *cobra.Command {
	var (
		flags  targetFlags
		output string
	)
	cmd := &cobra.Command{
		Use:   k.name,
		Short: fmt.Sprintf("%sを yaml に書き出す", k.resourceLabel),
		Long: fmt.Sprintf("指定した project / location の %sを読み取り、yaml ファイルに書き出す。\n", k.resourceLabel) +
			"書き出し先は既定で --file と同じ。--output に - を指定すると標準出力へ書き出す。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// export は書き出し先の既存ファイルから project / location と
			// ignore_change を引き継ぐ。
			existing, err := k.loadFile(flags.file)
			if err != nil {
				return err
			}
			project, location := existing.GetProject(), existing.GetLocation()
			if flags.project != "" {
				project = flags.project
			}
			if flags.location != "" {
				location = flags.location
			}
			if project == "" {
				return fmt.Errorf("project が指定されていません。--project を指定してください")
			}
			if location == "" {
				return fmt.Errorf("location が指定されていません。--location を指定してください")
			}

			ctx := cmd.Context()
			svc, err := k.newService(ctx)
			if err != nil {
				return err
			}
			defer svc.Close()

			items, err := svc.List(ctx, project, location)
			if err != nil {
				return err
			}

			ignoreByItem := map[string][]string{}
			for _, e := range existing.GetItems() {
				if len(e.ItemIgnoreChange()) > 0 {
					ignoreByItem[e.ItemName()] = e.ItemIgnoreChange()
				}
			}
			for _, item := range items {
				item.SetItemIgnoreChange(ignoreByItem[item.ItemName()])
			}

			dest := output
			if dest == "" {
				dest = flags.file
			}
			out := k.newFile()
			out.SetProject(project)
			out.SetLocation(location)
			out.SetIgnoreChange(existing.GetIgnoreChange())
			out.SetItems(items)
			out.Sort()
			if err := out.Save(dest); err != nil {
				return err
			}
			if dest != "-" {
				fmt.Fprintf(cmd.OutOrStdout(), "%d 件の%sを %s に書き出しました。\n", len(items), k.itemLabel, dest)
			}
			// 注記は --output - のときに yaml を汚さないよう標準エラーへ出す。
			if n, ok := svc.(resource.Notes); ok {
				for _, note := range n.Notes() {
					fmt.Fprintf(cmd.ErrOrStderr(), "note: %s\n", note)
				}
			}
			return nil
		},
	}
	flags.bind(cmd, k.defaultFile)
	cmd.Flags().StringVarP(&output, "output", "o", "", "書き出し先。既定は --file と同じ。- を指定すると標準出力へ書き出す")
	return cmd
}
