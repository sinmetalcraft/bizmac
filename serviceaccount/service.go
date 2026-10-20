package serviceaccount

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/sinmetalcraft/bizmac/prop"
	crm "google.golang.org/api/cloudresourcemanager/v1"
	"google.golang.org/api/googleapi"
	iamapi "google.golang.org/api/iam/v1"
)

// policyVersion は GetIamPolicy で要求するポリシーのバージョン。
// 3 を要求しないと条件付きの割り当てが条件無しに見えてしまう。
const policyVersion = 3

// Service は IAM API と Resource Manager API のクライアント。
type Service struct {
	iam *iamapi.Service
	crm *crm.Service
	// scope は管理対象のアカウント ID。scoped が false なら全件を対象にする。
	scope  map[string]bool
	scoped bool
	// notes は直近の List で除外したものなどの注記。
	notes []string
	// retryWait は作成直後のサービスアカウントがまだ見えないときの待ち時間。
	retryWait time.Duration
}

// NewService は Application Default Credentials でクライアントを作る。
func NewService(ctx context.Context) (*Service, error) {
	i, err := iamapi.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("serviceaccount: create iam client: %w", err)
	}
	c, err := crm.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("serviceaccount: create resource manager client: %w", err)
	}
	return &Service{iam: i, crm: c, retryWait: 2 * time.Second}, nil
}

// Close は resource.Service の実装。REST クライアントは閉じるものが無い。
func (s *Service) Close() error { return nil }

// Notes は直近の List で除外したものなどの注記を返す。
func (s *Service) Notes() []string { return s.notes }

// SetScope は resource.Scoped の実装。List は names のサービスアカウントだけを返す。
func (s *Service) SetScope(names []string) {
	s.scoped = true
	s.scope = make(map[string]bool, len(names))
	for _, n := range names {
		s.scope[n] = true
	}
}

// List は管理対象のサービスアカウントを、IAM の割り当てを含めて名前順で返す。
// location は使わない。
func (s *Service) List(ctx context.Context, project, _ string) ([]*ServiceAccount, error) {
	s.notes = nil
	var (
		sas     []*ServiceAccount
		skipped []string
	)
	suffix := "@" + project + ".iam.gserviceaccount.com"
	err := s.iam.Projects.ServiceAccounts.List("projects/"+project).Context(ctx).
		Pages(ctx, func(resp *iamapi.ListServiceAccountsResponse) error {
			for _, a := range resp.Accounts {
				name, ok := strings.CutSuffix(a.Email, suffix)
				if !ok {
					// Compute Engine や App Engine の既定のサービスアカウントは
					// アカウント ID を指定して作れないので管理対象にしない。
					skipped = append(skipped, a.Email)
					continue
				}
				if s.scoped && !s.scope[name] {
					continue
				}
				sas = append(sas, &ServiceAccount{
					Name:        name,
					DisplayName: a.DisplayName,
					Description: a.Description,
				})
			}
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("serviceaccount: list service accounts in %s: %w", project, err)
	}
	if !s.scoped && len(skipped) > 0 {
		sort.Strings(skipped)
		s.notes = append(s.notes, fmt.Sprintf("既定のサービスアカウント %d 件は管理対象外のため除外しました (%s)",
			len(skipped), strings.Join(skipped, ", ")))
	}
	if len(sas) == 0 {
		return nil, nil
	}

	policy, err := s.getProjectPolicy(ctx, project)
	if err != nil {
		return nil, err
	}
	for _, sa := range sas {
		roles, conditional := memberRoles(policy, Member(project, sa.Name))
		sa.ProjectRoles = roles
		if len(conditional) > 0 {
			s.notes = append(s.notes, fmt.Sprintf("%s: プロジェクトの条件付きの割り当ては管理対象外です (%s)",
				sa.Name, strings.Join(conditional, ", ")))
		}

		p, err := s.getAccountPolicy(ctx, project, sa.Name)
		if err != nil {
			return nil, err
		}
		members, n := policyMembers(p)
		sa.IAMPolicy = members
		if n > 0 {
			s.notes = append(s.notes, fmt.Sprintf("%s: サービスアカウントの条件付きの割り当て %d 件は管理対象外です", sa.Name, n))
		}
	}
	sort.Slice(sas, func(i, j int) bool { return sas[i].Name < sas[j].Name })
	return sas, nil
}

// Create はサービスアカウントを作り、IAM の割り当てを付ける。
func (s *Service) Create(ctx context.Context, project, _ string, sa *ServiceAccount) error {
	if _, err := s.iam.Projects.ServiceAccounts.Create("projects/"+project, &iamapi.CreateServiceAccountRequest{
		AccountId: sa.Name,
		ServiceAccount: &iamapi.ServiceAccount{
			DisplayName: sa.DisplayName,
			Description: sa.Description,
		},
	}).Context(ctx).Do(); err != nil {
		return fmt.Errorf("serviceaccount: create service account %q: %w", sa.Name, err)
	}
	// 作成直後のサービスアカウントは、しばらく IAM ポリシーのメンバーとして受け付けられない。
	return s.retryNew(ctx, func() error {
		if len(sa.ProjectRoles) > 0 {
			if err := s.applyProjectRoles(ctx, project, sa); err != nil {
				return err
			}
		}
		if len(sa.IAMPolicy) > 0 {
			if err := s.applyAccountPolicy(ctx, project, sa); err != nil {
				return err
			}
		}
		return nil
	})
}

// Update は既存のサービスアカウントを sa のとおりにする。
// changes から変わった部分を判断し、関係する API だけを呼ぶ。
func (s *Service) Update(ctx context.Context, project, _ string, sa *ServiceAccount, changes []prop.Change) error {
	var meta, roles, policy bool
	for _, c := range changes {
		switch top, _, _ := strings.Cut(c.Path, "."); top {
		case "display_name", "description":
			meta = true
		case "project_roles":
			roles = true
		case "iam_policy":
			policy = true
		}
	}
	if meta {
		a := &iamapi.ServiceAccount{
			DisplayName: sa.DisplayName,
			Description: sa.Description,
			// 空にする変更も送れるようにする。
			ForceSendFields: []string{"DisplayName", "Description"},
		}
		if _, err := s.iam.Projects.ServiceAccounts.Patch(accountPath(project, sa.Name), &iamapi.PatchServiceAccountRequest{
			ServiceAccount: a,
			UpdateMask:     "display_name,description",
		}).Context(ctx).Do(); err != nil {
			return fmt.Errorf("serviceaccount: update service account %q: %w", sa.Name, err)
		}
	}
	if roles {
		if err := s.applyProjectRoles(ctx, project, sa); err != nil {
			return err
		}
	}
	if policy {
		if err := s.applyAccountPolicy(ctx, project, sa); err != nil {
			return err
		}
	}
	return nil
}

// Delete は resource.Service の実装。serviceaccount はサービスアカウントの削除を行わない。
// yaml に書いたものだけを管理対象にするので、vacuum の削除候補が出ることは無い。
func (s *Service) Delete(_ context.Context, _, _, name string) error {
	return fmt.Errorf("serviceaccount: サービスアカウント %q の削除には対応していません。gcloud iam service-accounts delete で削除してください", name)
}

// applyProjectRoles はプロジェクトのポリシーで、sa に付いているロールを sa.ProjectRoles に
// そろえる。他のメンバーの割り当ては触らない。
func (s *Service) applyProjectRoles(ctx context.Context, project string, sa *ServiceAccount) error {
	member := Member(project, sa.Name)
	// プロジェクトのポリシーは他の経路からも書き換わるので、etag の競合なら読み直す。
	return retryConflict(ctx, func() error {
		p, err := s.getProjectPolicy(ctx, project)
		if err != nil {
			return err
		}
		if !setMemberRoles(p, member, sa.ProjectRoles) {
			return nil
		}
		if _, err := s.crm.Projects.SetIamPolicy(project, &crm.SetIamPolicyRequest{Policy: p}).Context(ctx).Do(); err != nil {
			return fmt.Errorf("serviceaccount: set iam policy of project %s for %q: %w", project, sa.Name, err)
		}
		return nil
	})
}

// applyAccountPolicy はサービスアカウント自体のポリシーを sa.IAMPolicy にそろえる。
func (s *Service) applyAccountPolicy(ctx context.Context, project string, sa *ServiceAccount) error {
	return retryConflict(ctx, func() error {
		p, err := s.getAccountPolicy(ctx, project, sa.Name)
		if err != nil {
			return err
		}
		if !setPolicyMembers(p, sa.IAMPolicy) {
			return nil
		}
		if _, err := s.iam.Projects.ServiceAccounts.SetIamPolicy(accountPath(project, sa.Name), &iamapi.SetIamPolicyRequest{
			Policy: p,
		}).Context(ctx).Do(); err != nil {
			return fmt.Errorf("serviceaccount: set iam policy of service account %q: %w", sa.Name, err)
		}
		return nil
	})
}

func (s *Service) getProjectPolicy(ctx context.Context, project string) (*crm.Policy, error) {
	p, err := s.crm.Projects.GetIamPolicy(project, &crm.GetIamPolicyRequest{
		Options: &crm.GetPolicyOptions{RequestedPolicyVersion: policyVersion},
	}).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("serviceaccount: get iam policy of project %s: %w", project, err)
	}
	return p, nil
}

func (s *Service) getAccountPolicy(ctx context.Context, project, name string) (*iamapi.Policy, error) {
	p, err := s.iam.Projects.ServiceAccounts.GetIamPolicy(accountPath(project, name)).
		OptionsRequestedPolicyVersion(policyVersion).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("serviceaccount: get iam policy of service account %q: %w", name, err)
	}
	return p, nil
}

func accountPath(project, name string) string {
	return "projects/" + project + "/serviceAccounts/" + Email(project, name)
}

// retryNew は作成直後のサービスアカウントがまだ見えないことによる失敗を待って再試行する。
func (s *Service) retryNew(ctx context.Context, fn func() error) error {
	wait := s.retryWait
	var err error
	for range 6 {
		if err = fn(); err == nil || !isNotYetVisible(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
		wait *= 2
	}
	return err
}

// isNotYetVisible は作成直後のサービスアカウントがまだ伝搬していないときのエラーかを返す。
// ポリシーのメンバーに書くと 400 で "does not exist"、ポリシーを読むと 404 になる。
func isNotYetVisible(err error) bool {
	var gerr *googleapi.Error
	if !errors.As(err, &gerr) {
		return false
	}
	switch gerr.Code {
	case http.StatusNotFound:
		return true
	case http.StatusBadRequest:
		return strings.Contains(gerr.Message, "does not exist")
	}
	return false
}

// retryConflict は IAM ポリシーの etag 競合 (409) なら読み直して再試行する。
func retryConflict(ctx context.Context, fn func() error) error {
	var err error
	for range 3 {
		err = fn()
		var gerr *googleapi.Error
		if err == nil || !errors.As(err, &gerr) || gerr.Code != http.StatusConflict {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return err
}
