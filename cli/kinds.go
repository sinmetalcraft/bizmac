package cli

import (
	"context"

	"github.com/sinmetalcraft/bizmac/cloudtasks"
	"github.com/sinmetalcraft/bizmac/dns"
	"github.com/sinmetalcraft/bizmac/resource"
	"github.com/sinmetalcraft/bizmac/scheduler"
	"github.com/sinmetalcraft/bizmac/serviceaccount"
)

// schedulerKind は Cloud Scheduler のジョブ。
func schedulerKind() kind[*scheduler.Job] {
	return kind[*scheduler.Job]{
		name:          "scheduler",
		itemLabel:     "ジョブ",
		resourceLabel: "Cloud Scheduler のジョブ",
		defaultFile:   scheduler.DefaultFileName,
		newFile:       func() resource.File[*scheduler.Job] { return &scheduler.File{} },
		loadFile: func(path string) (resource.File[*scheduler.Job], error) {
			return scheduler.LoadFile(path)
		},
		newItem: func() *scheduler.Job { return &scheduler.Job{} },
		newService: func(ctx context.Context) (resource.Service[*scheduler.Job], error) {
			return scheduler.NewService(ctx)
		},
	}
}

// cloudtasksKind は Cloud Tasks のキュー。
func cloudtasksKind() kind[*cloudtasks.Queue] {
	return kind[*cloudtasks.Queue]{
		name:          "cloudtasks",
		itemLabel:     "キュー",
		resourceLabel: "Cloud Tasks のキュー",
		defaultFile:   cloudtasks.DefaultFileName,
		newFile:       func() resource.File[*cloudtasks.Queue] { return &cloudtasks.File{} },
		loadFile: func(path string) (resource.File[*cloudtasks.Queue], error) {
			return cloudtasks.LoadFile(path)
		},
		newItem: func() *cloudtasks.Queue { return &cloudtasks.Queue{} },
		newService: func(ctx context.Context) (resource.Service[*cloudtasks.Queue], error) {
			return cloudtasks.NewService(ctx)
		},
		vacuumWarning: "キューを削除すると中のタスクは失われ、同じ名前のキューを 7 日間作り直せません。",
	}
}

// serviceaccountKind はサービスアカウントと IAM の割り当て。
func serviceaccountKind() kind[*serviceaccount.ServiceAccount] {
	return kind[*serviceaccount.ServiceAccount]{
		name:          "serviceaccount",
		itemLabel:     "サービスアカウント",
		resourceLabel: "サービスアカウントと IAM の割り当て",
		defaultFile:   serviceaccount.DefaultFileName,
		global:        true,
		newFile:       func() resource.File[*serviceaccount.ServiceAccount] { return &serviceaccount.File{} },
		loadFile: func(path string) (resource.File[*serviceaccount.ServiceAccount], error) {
			return serviceaccount.LoadFile(path)
		},
		newItem: func() *serviceaccount.ServiceAccount { return &serviceaccount.ServiceAccount{} },
		newService: func(ctx context.Context) (resource.Service[*serviceaccount.ServiceAccount], error) {
			return serviceaccount.NewService(ctx)
		},
		vacuumWarning: "割り当てを取り消すと、そのロールに頼っている処理が権限エラーになります。",
	}
}

// dnsKind は Cloud DNS のレコード。
func dnsKind() kind[*dns.RecordSet] {
	return kind[*dns.RecordSet]{
		name:          "dns",
		itemLabel:     "レコード",
		resourceLabel: "Cloud DNS のレコード",
		defaultFile:   dns.DefaultFileName,
		global:        true,
		newFile:       func() resource.File[*dns.RecordSet] { return &dns.File{} },
		loadFile: func(path string) (resource.File[*dns.RecordSet], error) {
			return dns.LoadFile(path)
		},
		newItem: func() *dns.RecordSet { return &dns.RecordSet{} },
		newService: func(ctx context.Context) (resource.Service[*dns.RecordSet], error) {
			return dns.NewService(ctx)
		},
		vacuumWarning: "レコードを削除すると、その名前は引けなくなります。キャッシュには TTL の間残ります。",
	}
}
