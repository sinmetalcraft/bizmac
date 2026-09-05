package cloudtasks

import (
	"fmt"
	"path"
	"sort"

	cloudtaskspb "cloud.google.com/go/cloudtasks/apiv2beta3/cloudtaskspb"
)

// QueuePath はキューのフルリソース名を組み立てる。
func QueuePath(project, location, name string) string {
	return fmt.Sprintf("projects/%s/locations/%s/queues/%s", project, location, name)
}

// LocationPath はキューの親リソース名を組み立てる。
func LocationPath(project, location string) string {
	return fmt.Sprintf("projects/%s/locations/%s", project, location)
}

// ToProto は yaml のキュー定義を Cloud Tasks API の Queue へ変換する。
func (q *Queue) ToProto(project, location string) (*cloudtaskspb.Queue, error) {
	pb := &cloudtaskspb.Queue{
		Name: QueuePath(project, location, q.Name),
	}

	taskTTL, err := parseDuration("task_ttl", q.TaskTTL)
	if err != nil {
		return nil, err
	}
	pb.TaskTtl = taskTTL

	tombstoneTTL, err := parseDuration("tombstone_ttl", q.TombstoneTTL)
	if err != nil {
		return nil, err
	}
	pb.TombstoneTtl = tombstoneTTL

	if rl := q.RateLimits; rl != nil {
		pb.RateLimits = &cloudtaskspb.RateLimits{}
		if rl.MaxDispatchesPerSecond != nil {
			pb.RateLimits.MaxDispatchesPerSecond = *rl.MaxDispatchesPerSecond
		}
		if rl.MaxConcurrentDispatches != nil {
			pb.RateLimits.MaxConcurrentDispatches = *rl.MaxConcurrentDispatches
		}
	}

	if rc := q.RetryConfig; rc != nil {
		maxRetry, err := parseDuration("retry_config.max_retry_duration", rc.MaxRetryDuration)
		if err != nil {
			return nil, err
		}
		minBackoff, err := parseDuration("retry_config.min_backoff", rc.MinBackoff)
		if err != nil {
			return nil, err
		}
		maxBackoff, err := parseDuration("retry_config.max_backoff", rc.MaxBackoff)
		if err != nil {
			return nil, err
		}
		pb.RetryConfig = &cloudtaskspb.RetryConfig{
			MaxRetryDuration: maxRetry,
			MinBackoff:       minBackoff,
			MaxBackoff:       maxBackoff,
		}
		if rc.MaxAttempts != nil {
			pb.RetryConfig.MaxAttempts = *rc.MaxAttempts
		}
		if rc.MaxDoublings != nil {
			pb.RetryConfig.MaxDoublings = *rc.MaxDoublings
		}
	}

	if lc := q.StackdriverLoggingConfig; lc != nil {
		pb.StackdriverLoggingConfig = &cloudtaskspb.StackdriverLoggingConfig{}
		if lc.SamplingRatio != nil {
			pb.StackdriverLoggingConfig.SamplingRatio = *lc.SamplingRatio
		}
	}

	if aq := q.AppEngineHTTPQueue; aq != nil {
		routing := &cloudtaskspb.AppEngineHttpQueue{}
		if r := aq.AppEngineRoutingOverride; r != nil {
			routing.AppEngineRoutingOverride = &cloudtaskspb.AppEngineRouting{
				Service:  r.Service,
				Version:  r.Version,
				Instance: r.Instance,
				Host:     r.Host,
			}
		}
		pb.QueueType = &cloudtaskspb.Queue_AppEngineHttpQueue{AppEngineHttpQueue: routing}
	}

	if t := q.HTTPTarget; t != nil {
		ht, err := t.toProto()
		if err != nil {
			return nil, err
		}
		pb.HttpTarget = ht
	}

	return pb, nil
}

func (t *HTTPTarget) toProto() (*cloudtaskspb.HttpTarget, error) {
	method, err := parseHTTPMethod(t.HTTPMethod)
	if err != nil {
		return nil, err
	}
	ht := &cloudtaskspb.HttpTarget{HttpMethod: method}

	if o := t.URIOverride; o != nil {
		uo := &cloudtaskspb.UriOverride{}
		if o.Scheme != "" {
			v, ok := cloudtaskspb.UriOverride_Scheme_value[o.Scheme]
			if !ok {
				return nil, fmt.Errorf("http_target.uri_override.scheme: %q is not a valid scheme (HTTP or HTTPS)", o.Scheme)
			}
			scheme := cloudtaskspb.UriOverride_Scheme(v)
			uo.Scheme = &scheme
		}
		if o.Host != "" {
			host := o.Host
			uo.Host = &host
		}
		if o.Port != nil {
			port := *o.Port
			uo.Port = &port
		}
		if o.PathOverride != "" {
			uo.PathOverride = &cloudtaskspb.PathOverride{Path: o.PathOverride}
		}
		if o.QueryOverride != "" {
			uo.QueryOverride = &cloudtaskspb.QueryOverride{QueryParams: o.QueryOverride}
		}
		if o.URIOverrideEnforceMode != "" {
			v, ok := cloudtaskspb.UriOverride_UriOverrideEnforceMode_value[o.URIOverrideEnforceMode]
			if !ok {
				return nil, fmt.Errorf("http_target.uri_override.uri_override_enforce_mode: %q is not a valid mode "+
					"(ALWAYS or IF_NOT_EXISTS)", o.URIOverrideEnforceMode)
			}
			uo.UriOverrideEnforceMode = cloudtaskspb.UriOverride_UriOverrideEnforceMode(v)
		}
		ht.UriOverride = uo
	}

	// header_overrides は map で書くが API では配列なので、キー順に並べて送る。
	if len(t.HeaderOverrides) > 0 {
		keys := make([]string, 0, len(t.HeaderOverrides))
		for k := range t.HeaderOverrides {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			ht.HeaderOverrides = append(ht.HeaderOverrides, &cloudtaskspb.HttpTarget_HeaderOverride{
				Header: &cloudtaskspb.HttpTarget_Header{Key: k, Value: t.HeaderOverrides[k]},
			})
		}
	}

	switch {
	case t.OAuthToken != nil && t.OIDCToken != nil:
		return nil, fmt.Errorf("http_target: oauth_token and oidc_token are mutually exclusive")
	case t.OAuthToken != nil:
		ht.AuthorizationHeader = &cloudtaskspb.HttpTarget_OauthToken{OauthToken: &cloudtaskspb.OAuthToken{
			ServiceAccountEmail: t.OAuthToken.ServiceAccountEmail,
			Scope:               t.OAuthToken.Scope,
		}}
	case t.OIDCToken != nil:
		ht.AuthorizationHeader = &cloudtaskspb.HttpTarget_OidcToken{OidcToken: &cloudtaskspb.OidcToken{
			ServiceAccountEmail: t.OIDCToken.ServiceAccountEmail,
			Audience:            t.OIDCToken.Audience,
		}}
	}
	return ht, nil
}

// QueueFromProto は Cloud Tasks API の Queue を yaml のキュー定義へ変換する。
// state や purge_time、rate_limits.max_burst_size などの
// 出力専用フィールドと、別 API でしか変えられないフィールドは落とす。
func QueueFromProto(pb *cloudtaskspb.Queue) *Queue {
	q := &Queue{
		Name:         path.Base(pb.GetName()),
		TaskTTL:      formatDuration(pb.GetTaskTtl()),
		TombstoneTTL: formatDuration(pb.GetTombstoneTtl()),
	}

	if rl := pb.GetRateLimits(); rl != nil {
		dispatches := rl.GetMaxDispatchesPerSecond()
		concurrent := rl.GetMaxConcurrentDispatches()
		q.RateLimits = &RateLimits{
			MaxDispatchesPerSecond:  &dispatches,
			MaxConcurrentDispatches: &concurrent,
		}
	}

	if rc := pb.GetRetryConfig(); rc != nil {
		attempts := rc.GetMaxAttempts()
		doublings := rc.GetMaxDoublings()
		q.RetryConfig = &RetryConfig{
			MaxAttempts:      &attempts,
			MaxRetryDuration: formatDuration(rc.GetMaxRetryDuration()),
			MinBackoff:       formatDuration(rc.GetMinBackoff()),
			MaxBackoff:       formatDuration(rc.GetMaxBackoff()),
			MaxDoublings:     &doublings,
		}
	}

	if lc := pb.GetStackdriverLoggingConfig(); lc != nil {
		ratio := lc.GetSamplingRatio()
		q.StackdriverLoggingConfig = &StackdriverLoggingConfig{SamplingRatio: &ratio}
	}

	if aq := pb.GetAppEngineHttpQueue(); aq != nil {
		out := &AppEngineHTTPQueue{}
		if r := aq.GetAppEngineRoutingOverride(); r != nil {
			out.AppEngineRoutingOverride = &AppEngineRouting{
				Service:  r.GetService(),
				Version:  r.GetVersion(),
				Instance: r.GetInstance(),
				Host:     r.GetHost(),
			}
		}
		q.AppEngineHTTPQueue = out
	}

	if t := pb.GetHttpTarget(); t != nil {
		q.HTTPTarget = httpTargetFromProto(t)
	}

	return q
}

func httpTargetFromProto(pb *cloudtaskspb.HttpTarget) *HTTPTarget {
	t := &HTTPTarget{HTTPMethod: formatHTTPMethod(pb.GetHttpMethod())}

	if o := pb.GetUriOverride(); o != nil {
		uo := &URIOverride{
			Host:                   o.GetHost(),
			PathOverride:           o.GetPathOverride().GetPath(),
			QueryOverride:          o.GetQueryOverride().GetQueryParams(),
			URIOverrideEnforceMode: formatEnforceMode(o.GetUriOverrideEnforceMode()),
		}
		if o.Scheme != nil {
			uo.Scheme = o.GetScheme().String()
		}
		if o.Port != nil {
			port := o.GetPort()
			uo.Port = &port
		}
		t.URIOverride = uo
	}

	if len(pb.GetHeaderOverrides()) > 0 {
		t.HeaderOverrides = make(map[string]string, len(pb.GetHeaderOverrides()))
		for _, h := range pb.GetHeaderOverrides() {
			t.HeaderOverrides[h.GetHeader().GetKey()] = h.GetHeader().GetValue()
		}
	}

	if o := pb.GetOauthToken(); o != nil {
		t.OAuthToken = &OAuthToken{ServiceAccountEmail: o.GetServiceAccountEmail(), Scope: o.GetScope()}
	}
	if o := pb.GetOidcToken(); o != nil {
		t.OIDCToken = &OIDCToken{ServiceAccountEmail: o.GetServiceAccountEmail(), Audience: o.GetAudience()}
	}
	return t
}

func parseHTTPMethod(v string) (cloudtaskspb.HttpMethod, error) {
	if v == "" {
		return cloudtaskspb.HttpMethod_HTTP_METHOD_UNSPECIFIED, nil
	}
	m, ok := cloudtaskspb.HttpMethod_value[v]
	if !ok {
		return 0, fmt.Errorf("http_target.http_method: %q is not a valid HTTP method", v)
	}
	return cloudtaskspb.HttpMethod(m), nil
}

func formatHTTPMethod(m cloudtaskspb.HttpMethod) string {
	if m == cloudtaskspb.HttpMethod_HTTP_METHOD_UNSPECIFIED {
		return ""
	}
	return m.String()
}

func formatEnforceMode(m cloudtaskspb.UriOverride_UriOverrideEnforceMode) string {
	if m == cloudtaskspb.UriOverride_URI_OVERRIDE_ENFORCE_MODE_UNSPECIFIED {
		return ""
	}
	return m.String()
}
