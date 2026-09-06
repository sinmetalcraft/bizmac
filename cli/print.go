package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/sinmetalcraft/bizmac/resource"
)

// planFormat は Plan の表示に使う文言。
// Google Cloud との比較とファイル同士の比較で切り替える。
type planFormat struct {
	create  func(name string) string
	update  func(name string) string
	delete  func(name string) string
	summary func(create, update, noChange, delete int) string
}

// apiPlanFormat は yaml と Google Cloud を比較するときの文言。
var apiPlanFormat = planFormat{
	create: func(name string) string { return "+ create " + name },
	update: func(name string) string { return "~ update " + name },
	delete: func(name string) string { return "- vacuum " + name + " (yaml に定義がありません)" },
	summary: func(create, update, noChange, del int) string {
		return fmt.Sprintf("create: %d, update: %d, no change: %d, vacuum candidate: %d",
			create, update, noChange, del)
	},
}

// filePlanFormat は yaml 同士を比較するときの文言。
// ファイル名は見出しに出るので、本体では --file / --against で参照する。
var filePlanFormat = planFormat{
	create: func(name string) string { return "+ " + name + " (--file にのみあります)" },
	update: func(name string) string { return "~ " + name },
	delete: func(name string) string { return "- " + name + " (--against にのみあります)" },
	summary: func(create, update, noChange, del int) string {
		return fmt.Sprintf("only in --file: %d, different: %d, same: %d, only in --against: %d",
			create, update, noChange, del)
	},
}

// printHeader は project / location と注記を出力する。
func printHeader[T resource.Item](w io.Writer, p *resource.Plan[T]) {
	fmt.Fprintf(w, "project:  %s\n", p.Project)
	fmt.Fprintf(w, "location: %s\n", p.Location)
	for _, note := range p.Notes {
		fmt.Fprintf(w, "note: %s\n", note)
	}
	fmt.Fprintln(w)
}

// printPlan は yaml と Google Cloud の差分を人が読める形で出力する。
// 削除候補は削除せず、vacuum の対象としてだけ示す。
func printPlan[T resource.Item](w io.Writer, p *resource.Plan[T]) error {
	printHeader(w, p)
	return printPlanBody(w, p, apiPlanFormat)
}

// printFilePlan は yaml 同士の差分を人が読める形で出力する。
func printFilePlan[T resource.Item](w io.Writer, filePath, againstPath string, fp *filePlan[T]) error {
	fmt.Fprintf(w, "file:    %s (project: %s, location: %s)\n",
		filePath, orDash(fp.file.GetProject()), orDash(fp.file.GetLocation()))
	fmt.Fprintf(w, "against: %s (project: %s, location: %s)\n\n",
		againstPath, orDash(fp.against.GetProject()), orDash(fp.against.GetLocation()))
	return printPlanBody(w, fp.plan, filePlanFormat)
}

func orDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

func printPlanBody[T resource.Item](w io.Writer, p *resource.Plan[T], f planFormat) error {
	for _, item := range p.Create {
		fmt.Fprintln(w, f.create(item.ItemName()))
		body, err := resource.MarshalItem(item)
		if err != nil {
			return err
		}
		fmt.Fprint(w, indent(body, "    "))
	}

	for _, u := range p.Update {
		fmt.Fprintln(w, f.update(u.Name))
		if u.RecreateRequired {
			fmt.Fprintf(w, "    ! 種別が %s から %s へ変わっています。update では変更できないので、"+
				"一度削除して作り直してください\n", u.Actual.RecreateKey(), u.Desired.RecreateKey())
		}
		for _, c := range u.Changes {
			fmt.Fprintf(w, "    %s\n", c)
		}
	}

	for _, item := range p.Delete {
		fmt.Fprintln(w, f.delete(item.ItemName()))
	}

	if len(p.Create) == 0 && len(p.Update) == 0 && len(p.Delete) == 0 {
		fmt.Fprintln(w, "差分はありません。")
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, f.summary(len(p.Create), len(p.Update), len(p.NoChange), len(p.Delete)))
	return nil
}

func indent(s, pad string) string {
	var sb strings.Builder
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		sb.WriteString(pad)
		sb.WriteString(line)
		sb.WriteString("\n")
	}
	return sb.String()
}
