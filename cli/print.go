package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/sinmetalcraft/bizmac/resource"
)

// printHeader は project / location と注記を出力する。
func printHeader[T resource.Item](w io.Writer, p *resource.Plan[T]) {
	fmt.Fprintf(w, "project:  %s\n", p.Project)
	fmt.Fprintf(w, "location: %s\n", p.Location)
	for _, note := range p.Notes {
		fmt.Fprintf(w, "note: %s\n", note)
	}
	fmt.Fprintln(w)
}

// printPlan は Plan を人が読める形で出力する。
// 削除候補は削除せず、vacuum の対象としてだけ示す。
func printPlan[T resource.Item](w io.Writer, p *resource.Plan[T]) error {
	printHeader(w, p)

	for _, item := range p.Create {
		fmt.Fprintf(w, "+ create %s\n", item.ItemName())
		body, err := resource.MarshalItem(item)
		if err != nil {
			return err
		}
		fmt.Fprint(w, indent(body, "    "))
	}

	for _, u := range p.Update {
		fmt.Fprintf(w, "~ update %s\n", u.Name)
		if u.RecreateRequired {
			fmt.Fprintf(w, "    ! 種別が %s から %s へ変わっています。update では変更できないので、"+
				"一度削除して作り直してください\n", u.Actual.RecreateKey(), u.Desired.RecreateKey())
		}
		for _, c := range u.Changes {
			fmt.Fprintf(w, "    %s\n", c)
		}
	}

	for _, item := range p.Delete {
		fmt.Fprintf(w, "- vacuum %s (yaml に定義がありません)\n", item.ItemName())
	}

	if len(p.Create) == 0 && len(p.Update) == 0 && len(p.Delete) == 0 {
		fmt.Fprintln(w, "差分はありません。")
	}

	fmt.Fprintf(w, "\ncreate: %d, update: %d, no change: %d, vacuum candidate: %d\n",
		len(p.Create), len(p.Update), len(p.NoChange), len(p.Delete))
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
