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
	revoke  func(name string) string
	summary func(create, update, noChange, delete int) string
}

// apiPlanFormat は yaml と Google Cloud を比較するときの文言。
var apiPlanFormat = planFormat{
	create: func(name string) string { return "+ create " + name },
	update: func(name string) string { return "~ update " + name },
	delete: func(name string) string { return "- vacuum " + name + " (yaml に定義がありません)" },
	revoke: func(name string) string { return "- vacuum " + name + " (yaml に無い割り当て)" },
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
	revoke: func(name string) string { return "- " + name + " (--against にのみある割り当て)" },
	summary: func(create, update, noChange, del int) string {
		return fmt.Sprintf("only in --file: %d, different: %d, same: %d, only in --against: %d",
			create, update, noChange, del)
	},
}

// printHeader は project / location と注記を出力する。
func printHeader[T resource.Item](w io.Writer, p *resource.Plan[T]) {
	fmt.Fprintf(w, "project:  %s\n", p.Project)
	// IAM のようにロケーションを持たないリソースでは出さない。
	if p.Location != "" {
		fmt.Fprintf(w, "location: %s\n", p.Location)
	}
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
	fmt.Fprintf(w, "file:    %s (%s)\n", filePath, describeTarget(fp.file, fp.global))
	fmt.Fprintf(w, "against: %s (%s)\n\n", againstPath, describeTarget(fp.against, fp.global))
	return printPlanBody(w, fp.plan, filePlanFormat)
}

// describeTarget は "project: xxx, location: yyy" を返す。
// ロケーションを持たないリソースでは location を省く。
func describeTarget[T resource.Item](f resource.File[T], global bool) string {
	s := "project: " + orDash(f.GetProject())
	if !global {
		s += ", location: " + orDash(f.GetLocation())
	}
	return s
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

	for _, r := range p.Revoke {
		fmt.Fprintln(w, f.revoke(r.Name))
		for _, c := range r.Changes {
			fmt.Fprintf(w, "    %s\n", c)
		}
	}

	if !p.HasChange() && !p.HasVacuum() {
		fmt.Fprintln(w, "差分はありません。")
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, f.summary(len(p.Create), len(p.Update), len(p.NoChange), len(p.Delete)+len(p.Revoke)))
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
