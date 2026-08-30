package scheduler

import (
	"fmt"
	"path"
	"time"

	schedulerpb "cloud.google.com/go/scheduler/apiv1/schedulerpb"
	"google.golang.org/protobuf/types/known/durationpb"
)

// JobPath はジョブのフルリソース名を組み立てる。
func JobPath(project, location, name string) string {
	return fmt.Sprintf("projects/%s/locations/%s/jobs/%s", project, location, name)
}

// LocationPath はジョブの親リソース名を組み立てる。
func LocationPath(project, location string) string {
	return fmt.Sprintf("projects/%s/locations/%s", project, location)
}

// ToProto は yaml のジョブ定義を Cloud Scheduler API の Job へ変換する。
func (j *Job) ToProto(project, location string) (*schedulerpb.Job, error) {
	pb := &schedulerpb.Job{
		Name:        JobPath(project, location, j.Name),
		Description: j.Description,
		Schedule:    j.Schedule,
		TimeZone:    j.TimeZone,
	}

	d, err := parseDuration("attempt_deadline", j.AttemptDeadline)
	if err != nil {
		return nil, err
	}
	pb.AttemptDeadline = d

	if rc := j.RetryConfig; rc != nil {
		maxRetry, err := parseDuration("retry_config.max_retry_duration", rc.MaxRetryDuration)
		if err != nil {
			return nil, err
		}
		minBackoff, err := parseDuration("retry_config.min_backoff_duration", rc.MinBackoffDuration)
		if err != nil {
			return nil, err
		}
		maxBackoff, err := parseDuration("retry_config.max_backoff_duration", rc.MaxBackoffDuration)
		if err != nil {
			return nil, err
		}
		pb.RetryConfig = &schedulerpb.RetryConfig{
			RetryCount:         rc.RetryCount,
			MaxRetryDuration:   maxRetry,
			MinBackoffDuration: minBackoff,
			MaxBackoffDuration: maxBackoff,
			MaxDoublings:       rc.MaxDoublings,
		}
	}

	switch {
	case j.HTTPTarget != nil:
		t := j.HTTPTarget
		method, err := parseHTTPMethod(t.HTTPMethod)
		if err != nil {
			return nil, err
		}
		body, err := encodePayload(t.Body, t.BodyJSON)
		if err != nil {
			return nil, fmt.Errorf("http_target: %w", err)
		}
		ht := &schedulerpb.HttpTarget{
			Uri:        t.URI,
			HttpMethod: method,
			Headers:    t.Headers,
			Body:       body,
		}
		switch {
		case t.OAuthToken != nil && t.OIDCToken != nil:
			return nil, fmt.Errorf("http_target: oauth_token and oidc_token are mutually exclusive")
		case t.OAuthToken != nil:
			ht.AuthorizationHeader = &schedulerpb.HttpTarget_OauthToken{OauthToken: &schedulerpb.OAuthToken{
				ServiceAccountEmail: t.OAuthToken.ServiceAccountEmail,
				Scope:               t.OAuthToken.Scope,
			}}
		case t.OIDCToken != nil:
			ht.AuthorizationHeader = &schedulerpb.HttpTarget_OidcToken{OidcToken: &schedulerpb.OidcToken{
				ServiceAccountEmail: t.OIDCToken.ServiceAccountEmail,
				Audience:            t.OIDCToken.Audience,
			}}
		}
		pb.Target = &schedulerpb.Job_HttpTarget{HttpTarget: ht}

	case j.AppEngineHTTPTarget != nil:
		t := j.AppEngineHTTPTarget
		method, err := parseHTTPMethod(t.HTTPMethod)
		if err != nil {
			return nil, err
		}
		body, err := encodePayload(t.Body, t.BodyJSON)
		if err != nil {
			return nil, fmt.Errorf("app_engine_http_target: %w", err)
		}
		at := &schedulerpb.AppEngineHttpTarget{
			HttpMethod:  method,
			RelativeUri: t.RelativeURI,
			Headers:     t.Headers,
			Body:        body,
		}
		if r := t.AppEngineRouting; r != nil {
			at.AppEngineRouting = &schedulerpb.AppEngineRouting{
				Service:  r.Service,
				Version:  r.Version,
				Instance: r.Instance,
				Host:     r.Host,
			}
		}
		pb.Target = &schedulerpb.Job_AppEngineHttpTarget{AppEngineHttpTarget: at}

	case j.PubsubTarget != nil:
		t := j.PubsubTarget
		data, err := encodePayload(t.Data, t.DataJSON)
		if err != nil {
			return nil, fmt.Errorf("pubsub_target: %w", err)
		}
		pb.Target = &schedulerpb.Job_PubsubTarget{PubsubTarget: &schedulerpb.PubsubTarget{
			TopicName:  t.TopicName,
			Data:       data,
			Attributes: t.Attributes,
		}}

	default:
		return nil, fmt.Errorf("job %q has no target", j.Name)
	}

	return pb, nil
}

// JobFromProto は Cloud Scheduler API の Job を yaml のジョブ定義へ変換する。
// state や status などの出力専用フィールドは落とす。
func JobFromProto(pb *schedulerpb.Job) *Job {
	j := &Job{
		Name:            path.Base(pb.GetName()),
		Description:     pb.GetDescription(),
		Schedule:        pb.GetSchedule(),
		TimeZone:        pb.GetTimeZone(),
		AttemptDeadline: formatDuration(pb.GetAttemptDeadline()),
	}

	if rc := pb.GetRetryConfig(); rc != nil {
		j.RetryConfig = &RetryConfig{
			RetryCount:         rc.GetRetryCount(),
			MaxRetryDuration:   formatDuration(rc.GetMaxRetryDuration()),
			MinBackoffDuration: formatDuration(rc.GetMinBackoffDuration()),
			MaxBackoffDuration: formatDuration(rc.GetMaxBackoffDuration()),
			MaxDoublings:       rc.GetMaxDoublings(),
		}
	}

	switch {
	case pb.GetHttpTarget() != nil:
		t := pb.GetHttpTarget()
		body, bodyJSON := decodePayload(t.GetBody())
		ht := &HTTPTarget{
			URI:        t.GetUri(),
			HTTPMethod: formatHTTPMethod(t.GetHttpMethod()),
			Headers:    t.GetHeaders(),
			Body:       body,
			BodyJSON:   bodyJSON,
		}
		if o := t.GetOauthToken(); o != nil {
			ht.OAuthToken = &OAuthToken{ServiceAccountEmail: o.GetServiceAccountEmail(), Scope: o.GetScope()}
		}
		if o := t.GetOidcToken(); o != nil {
			ht.OIDCToken = &OIDCToken{ServiceAccountEmail: o.GetServiceAccountEmail(), Audience: o.GetAudience()}
		}
		j.HTTPTarget = ht

	case pb.GetAppEngineHttpTarget() != nil:
		t := pb.GetAppEngineHttpTarget()
		body, bodyJSON := decodePayload(t.GetBody())
		at := &AppEngineHTTPTarget{
			HTTPMethod:  formatHTTPMethod(t.GetHttpMethod()),
			RelativeURI: t.GetRelativeUri(),
			Headers:     t.GetHeaders(),
			Body:        body,
			BodyJSON:    bodyJSON,
		}
		if r := t.GetAppEngineRouting(); r != nil {
			at.AppEngineRouting = &AppEngineRouting{
				Service:  r.GetService(),
				Version:  r.GetVersion(),
				Instance: r.GetInstance(),
				Host:     r.GetHost(),
			}
		}
		j.AppEngineHTTPTarget = at

	case pb.GetPubsubTarget() != nil:
		t := pb.GetPubsubTarget()
		data, dataJSON := decodePayload(t.GetData())
		j.PubsubTarget = &PubsubTarget{
			TopicName:  t.GetTopicName(),
			Data:       data,
			DataJSON:   dataJSON,
			Attributes: t.GetAttributes(),
		}
	}

	return j
}

func parseDuration(field, v string) (*durationpb.Duration, error) {
	if v == "" {
		return nil, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return nil, fmt.Errorf("%s: %q is not a valid duration (e.g. \"3m0s\", \"180s\"): %w", field, v, err)
	}
	return durationpb.New(d), nil
}

func formatDuration(d *durationpb.Duration) string {
	if d == nil {
		return ""
	}
	return d.AsDuration().String()
}

func parseHTTPMethod(v string) (schedulerpb.HttpMethod, error) {
	if v == "" {
		return schedulerpb.HttpMethod_HTTP_METHOD_UNSPECIFIED, nil
	}
	m, ok := schedulerpb.HttpMethod_value[v]
	if !ok {
		return 0, fmt.Errorf("http_method: %q is not a valid HTTP method", v)
	}
	return schedulerpb.HttpMethod(m), nil
}

func formatHTTPMethod(m schedulerpb.HttpMethod) string {
	if m == schedulerpb.HttpMethod_HTTP_METHOD_UNSPECIFIED {
		return ""
	}
	return m.String()
}
