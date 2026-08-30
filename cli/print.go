package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/sinmetalcraft/bizmac/scheduler"
)

// printPlan は Plan を人が読める形で出力する。
// withDelete が false のとき、削除候補は vacuum の対象としてだけ示す。
func printPlan(w io.Writer, p *scheduler.Plan) error {
	fmt.Fprintf(w, "project:  %s\n", p.Project)
	fmt.Fprintf(w, "location: %s\n\n", p.Location)

	for _, j := range p.Create {
		fmt.Fprintf(w, "+ create %s\n", j.Name)
		body, err := marshalJob(j)
		if err != nil {
			return err
		}
		fmt.Fprint(w, indent(body, "  "))
	}

	for _, u := range p.Update {
		fmt.Fprintf(w, "~ update %s\n", u.Name)
		if u.TargetKindChanged {
			fmt.Fprintf(w, "    ! ターゲット種別が %s から %s へ変わっています。update では変更できないので、"+
				"一度削除して作り直してください\n", u.Actual.TargetKind(), u.Desired.TargetKind())
		}
		for _, c := range u.Changes {
			fmt.Fprintf(w, "    %s\n", c)
		}
	}

	for _, j := range p.Delete {
		fmt.Fprintf(w, "- vacuum %s (yaml に定義がありません)\n", j.Name)
	}

	if len(p.Create) == 0 && len(p.Update) == 0 && len(p.Delete) == 0 {
		fmt.Fprintln(w, "差分はありません。")
	}

	fmt.Fprintf(w, "\ncreate: %d, update: %d, no change: %d, vacuum candidate: %d\n",
		len(p.Create), len(p.Update), len(p.NoChange), len(p.Delete))
	return nil
}

// marshalJob は 1 ジョブを yaml へエンコードする。
// 出力は yaml のリスト要素として 2 文字インデントされた状態で返る。
func marshalJob(j *scheduler.Job) (string, error) {
	f := &scheduler.File{Jobs: []*scheduler.Job{j}}
	b, err := f.Marshal()
	if err != nil {
		return "", err
	}
	// File 全体としてエンコードされた "jobs:" 行を落として、ジョブ本体だけを残す。
	s := strings.TrimPrefix(string(b), "jobs:\n")
	return s, nil
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
