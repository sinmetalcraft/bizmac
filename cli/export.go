package cli

import (
	"fmt"

	"github.com/sinmetalcraft/bizmac/scheduler"
	"github.com/spf13/cobra"
)

func newExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Google Cloud の現在のリソースを yaml に書き出す",
	}
	cmd.AddCommand(newExportSchedulerCmd())
	return cmd
}

func newExportSchedulerCmd() *cobra.Command {
	var (
		flags  targetFlags
		output string
	)
	cmd := &cobra.Command{
		Use:   "scheduler",
		Short: "Cloud Scheduler のジョブを yaml に書き出す",
		Long: "指定した project / location の Cloud Scheduler のジョブを読み取り、yaml ファイルに書き出す。\n" +
			"書き出し先は既定で --file と同じ。--output に - を指定すると標準出力へ書き出す。",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// export は書き出し先の既存ファイルから project / location と
			// ignore_change を引き継ぐ。
			existing, err := scheduler.LoadFile(flags.file)
			if err != nil {
				return err
			}
			project, location := existing.Project, existing.Location
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
			svc, err := scheduler.NewService(ctx)
			if err != nil {
				return err
			}
			defer svc.Close()

			jobs, err := svc.List(ctx, project, location)
			if err != nil {
				return err
			}

			ignoreByJob := map[string][]string{}
			for _, j := range existing.Jobs {
				if len(j.IgnoreChange) > 0 {
					ignoreByJob[j.Name] = j.IgnoreChange
				}
			}
			for _, j := range jobs {
				j.IgnoreChange = ignoreByJob[j.Name]
			}

			dest := output
			if dest == "" {
				dest = flags.file
			}
			out := &scheduler.File{
				Project:      project,
				Location:     location,
				IgnoreChange: existing.IgnoreChange,
				Jobs:         jobs,
			}
			out.SortJobs()
			if err := out.Save(dest); err != nil {
				return err
			}
			if dest != "-" {
				fmt.Fprintf(cmd.OutOrStdout(), "%d 件のジョブを %s に書き出しました。\n", len(jobs), dest)
			}
			return nil
		},
	}
	flags.bind(cmd, scheduler.DefaultFileName)
	cmd.Flags().StringVarP(&output, "output", "o", "", "書き出し先。既定は --file と同じ。- を指定すると標準出力へ書き出す")
	return cmd
}
