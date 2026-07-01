package main

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func mustWriteRaw(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ---------------------------------------------------------------------------
// 1. deepMerge
// ---------------------------------------------------------------------------

func TestDeepMerge(t *testing.T) {
	tests := []struct {
		name     string
		base     map[string]interface{}
		override map[string]interface{}
		want     map[string]interface{}
	}{
		{
			name:     "empty override returns copy of base",
			base:     map[string]interface{}{"a": 1},
			override: map[string]interface{}{},
			want:     map[string]interface{}{"a": 1},
		},
		{
			name:     "override scalar wins",
			base:     map[string]interface{}{"a": 1},
			override: map[string]interface{}{"a": 2},
			want:     map[string]interface{}{"a": 2},
		},
		{
			name:     "override adds new key",
			base:     map[string]interface{}{"a": 1},
			override: map[string]interface{}{"b": 2},
			want:     map[string]interface{}{"a": 1, "b": 2},
		},
		{
			name: "nested maps are merged recursively",
			base: map[string]interface{}{
				"server": map[string]interface{}{"port": 8080, "debug": false},
			},
			override: map[string]interface{}{
				"server": map[string]interface{}{"debug": true},
			},
			want: map[string]interface{}{
				"server": map[string]interface{}{"port": 8080, "debug": true},
			},
		},
		{
			name: "array is replaced not appended",
			base: map[string]interface{}{
				"endpoints": []interface{}{"a", "b"},
			},
			override: map[string]interface{}{
				"endpoints": []interface{}{"c"},
			},
			want: map[string]interface{}{
				"endpoints": []interface{}{"c"},
			},
		},
		{
			name: "override map replaces scalar base",
			base: map[string]interface{}{"x": "scalar"},
			override: map[string]interface{}{
				"x": map[string]interface{}{"nested": true},
			},
			want: map[string]interface{}{
				"x": map[string]interface{}{"nested": true},
			},
		},
		{
			name: "deep three-level merge",
			base: map[string]interface{}{
				"a": map[string]interface{}{
					"b": map[string]interface{}{"c": 1, "d": 2},
				},
			},
			override: map[string]interface{}{
				"a": map[string]interface{}{
					"b": map[string]interface{}{"c": 99},
				},
			},
			want: map[string]interface{}{
				"a": map[string]interface{}{
					"b": map[string]interface{}{"c": 99, "d": 2},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deepMerge(tc.base, tc.override)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDeepMergeDoesNotMutateBase(t *testing.T) {
	base := map[string]interface{}{"a": 1}
	override := map[string]interface{}{"b": 2}
	_ = deepMerge(base, override)
	if _, ok := base["b"]; ok {
		t.Error("deepMerge mutated base map")
	}
}

// ---------------------------------------------------------------------------
// 2. hostnameFromURL
// ---------------------------------------------------------------------------

func TestHostnameFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"http://example.com/path", "example.com"},
		{"https://example.com:8080", "example.com"},
		{"http://myservice/health", "myservice"},
		{"tcp://db:5432", "db"},
		{"http://192.168.1.1/", "192.168.1.1"},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.url, func(t *testing.T) {
			got := hostnameFromURL(tc.url)
			if got != tc.want {
				t.Errorf("hostnameFromURL(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 3. toSliceOfMaps
// ---------------------------------------------------------------------------

func TestToSliceOfMaps(t *testing.T) {
	t.Run("valid slice of maps", func(t *testing.T) {
		input := []interface{}{
			map[string]interface{}{"name": "a"},
			map[string]interface{}{"name": "b"},
		}
		got, ok := toSliceOfMaps(input)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if len(got) != 2 {
			t.Fatalf("len=%d, want 2", len(got))
		}
	})

	t.Run("non-slice returns false", func(t *testing.T) {
		_, ok := toSliceOfMaps("not a slice")
		if ok {
			t.Fatal("expected ok=false")
		}
	})

	t.Run("nil returns false", func(t *testing.T) {
		_, ok := toSliceOfMaps(nil)
		if ok {
			t.Fatal("expected ok=false")
		}
	})

	t.Run("mixed slice skips non-maps", func(t *testing.T) {
		input := []interface{}{
			map[string]interface{}{"name": "a"},
			"not-a-map",
		}
		got, ok := toSliceOfMaps(input)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if len(got) != 1 {
			t.Fatalf("len=%d, want 1", len(got))
		}
	})
}

// ---------------------------------------------------------------------------
// 4. alertProviderList
// ---------------------------------------------------------------------------

func TestAlertProviderList(t *testing.T) {
	tests := []struct {
		name           string
		cfg            map[string]interface{}
		wantTypes      []string
		wantNilOrEmpty bool
	}{
		{
			name:           "no alerting key",
			cfg:            map[string]interface{}{"server": map[string]interface{}{}},
			wantNilOrEmpty: true,
		},
		{
			name:           "alerting is not a map",
			cfg:            map[string]interface{}{"alerting": "bad"},
			wantNilOrEmpty: true,
		},
		{
			name: "single provider",
			cfg: map[string]interface{}{
				"alerting": map[string]interface{}{
					"slack": map[string]interface{}{"webhook-url": "https://hooks.slack.com/x"},
				},
			},
			wantTypes: []string{"slack"},
		},
		{
			name: "multiple providers",
			cfg: map[string]interface{}{
				"alerting": map[string]interface{}{
					"slack": map[string]interface{}{},
					"ntfy":  map[string]interface{}{},
				},
			},
			wantTypes: []string{"ntfy", "slack"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := alertProviderList(tc.cfg)
			if tc.wantNilOrEmpty {
				if len(got) != 0 {
					t.Errorf("expected empty, got %v", got)
				}
				return
			}
			var types []string
			for _, p := range got {
				types = append(types, p["type"].(string))
			}
			sort.Strings(types)
			if !reflect.DeepEqual(types, tc.wantTypes) {
				t.Errorf("got %v, want %v", types, tc.wantTypes)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 5. Label discovery
//
// discoverEndpoints requires a live *client.Client. We mirror its loop in
// syntheticDiscover over fakeContainers so every branch is exercised without
// a Docker daemon.
// ---------------------------------------------------------------------------

type fakeContainer struct {
	Names  []string
	Labels map[string]string
}

// hasDot reports whether s contains a '.' character.
func hasDot(s string) bool {
	for _, c := range s {
		if c == '.' {
			return true
		}
	}
	return false
}

// splitWhitespace splits s on ASCII whitespace, mirroring strings.Fields.
func splitWhitespace(s string) []string {
	var result []string
	start := -1
	for i, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if start >= 0 {
				result = append(result, s[start:i])
				start = -1
			}
		} else if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		result = append(result, s[start:])
	}
	return result
}

// syntheticDiscover mirrors discoverEndpoints exactly on fakeContainers.
func syntheticDiscover(containers []fakeContainer, globalResolver, defaultInterval string) []map[string]interface{} {
	containerNames := make(map[string]bool)
	for _, c := range containers {
		for _, n := range c.Names {
			if len(n) > 0 && n[0] == '/' {
				n = n[1:]
			}
			containerNames[n] = true
		}
	}

	var endpoints []map[string]interface{}

	for _, c := range containers {
		labels := c.Labels
		urlLabel, ok := labels["gatus.io/url"]
		if !ok {
			continue
		}
		if labels["gatus.io/enabled"] == "false" {
			continue
		}

		var urls []string
		for _, f := range splitWhitespace(urlLabel) {
			if f != "" {
				urls = append(urls, f)
			}
		}
		if len(urls) == 0 {
			continue
		}

		interval := labels["gatus.io/interval"]
		if interval == "" {
			interval = defaultInterval
		}
		if interval == "" {
			interval = "1m"
		}

		conditions := labels["gatus.io/conditions"]
		if conditions == "" {
			conditions = "[STATUS] == 200"
		}

		labelResolver := labels["gatus.io/dns-resolver"]
		multi := len(urls) > 1

		containerName := ""
		if len(c.Names) > 0 {
			n := c.Names[0]
			if len(n) > 0 && n[0] == '/' {
				n = n[1:]
			}
			containerName = n
		}

		for _, u := range urls {
			host := hostnameFromURL(u)

			var effectiveResolver string
			if labelResolver != "" {
				effectiveResolver = labelResolver
			} else if hasDot(host) && !containerNames[host] && globalResolver != "" {
				effectiveResolver = globalResolver
			}

			name := containerName
			if multi {
				name = u
			}

			ep := map[string]interface{}{
				"name":       name,
				"url":        u,
				"interval":   interval,
				"conditions": []string{conditions},
			}
			if effectiveResolver != "" {
				ep["client"] = map[string]interface{}{
					"dns-resolver": effectiveResolver,
				}
			}

			endpoints = append(endpoints, ep)
		}
	}

	return endpoints
}

func TestLabelDiscovery(t *testing.T) {
	tests := []struct {
		name            string
		containers      []fakeContainer
		globalResolver  string
		defaultInterval string
		wantLen         int
		check           func(t *testing.T, eps []map[string]interface{})
	}{
		{
			name: "no gatus.io/url label produces no endpoints",
			containers: []fakeContainer{
				{Names: []string{"/myapp"}, Labels: map[string]string{}},
			},
			wantLen: 0,
		},
		{
			name: "enabled=false skipped",
			containers: []fakeContainer{
				{Names: []string{"/myapp"}, Labels: map[string]string{
					"gatus.io/url":     "http://myapp/health",
					"gatus.io/enabled": "false",
				}},
			},
			wantLen: 0,
		},
		{
			name: "single URL uses container name as endpoint name",
			containers: []fakeContainer{
				{Names: []string{"/webapp"}, Labels: map[string]string{
					"gatus.io/url": "http://webapp/health",
				}},
			},
			wantLen: 1,
			check: func(t *testing.T, eps []map[string]interface{}) {
				if eps[0]["name"] != "webapp" {
					t.Errorf("name=%q, want webapp", eps[0]["name"])
				}
				if eps[0]["url"] != "http://webapp/health" {
					t.Errorf("url=%q unexpected", eps[0]["url"])
				}
			},
		},
		{
			name: "multi-URL uses URL as endpoint name",
			containers: []fakeContainer{
				{Names: []string{"/svc"}, Labels: map[string]string{
					"gatus.io/url": "http://svc/a http://svc/b",
				}},
			},
			wantLen: 2,
			check: func(t *testing.T, eps []map[string]interface{}) {
				if eps[0]["name"] != "http://svc/a" {
					t.Errorf("ep0 name=%q, want url", eps[0]["name"])
				}
				if eps[1]["name"] != "http://svc/b" {
					t.Errorf("ep1 name=%q, want url", eps[1]["name"])
				}
			},
		},
		{
			name: "custom interval label honoured",
			containers: []fakeContainer{
				{Names: []string{"/svc"}, Labels: map[string]string{
					"gatus.io/url":      "http://svc/health",
					"gatus.io/interval": "5m",
				}},
			},
			wantLen: 1,
			check: func(t *testing.T, eps []map[string]interface{}) {
				if eps[0]["interval"] != "5m" {
					t.Errorf("interval=%q, want 5m", eps[0]["interval"])
				}
			},
		},
		{
			name:            "defaultInterval used when label absent",
			defaultInterval: "30s",
			containers: []fakeContainer{
				{Names: []string{"/svc"}, Labels: map[string]string{
					"gatus.io/url": "http://svc/health",
				}},
			},
			wantLen: 1,
			check: func(t *testing.T, eps []map[string]interface{}) {
				if eps[0]["interval"] != "30s" {
					t.Errorf("interval=%q, want 30s", eps[0]["interval"])
				}
			},
		},
		{
			name: "fallback interval is 1m when neither label nor default is set",
			containers: []fakeContainer{
				{Names: []string{"/svc"}, Labels: map[string]string{
					"gatus.io/url": "http://svc/health",
				}},
			},
			wantLen: 1,
			check: func(t *testing.T, eps []map[string]interface{}) {
				if eps[0]["interval"] != "1m" {
					t.Errorf("interval=%q, want 1m", eps[0]["interval"])
				}
			},
		},
		{
			name: "custom conditions label honoured",
			containers: []fakeContainer{
				{Names: []string{"/svc"}, Labels: map[string]string{
					"gatus.io/url":        "http://svc/health",
					"gatus.io/conditions": "[STATUS] == 204",
				}},
			},
			wantLen: 1,
			check: func(t *testing.T, eps []map[string]interface{}) {
				conds := eps[0]["conditions"].([]string)
				if len(conds) != 1 || conds[0] != "[STATUS] == 204" {
					t.Errorf("conditions=%v", conds)
				}
			},
		},
		{
			name:           "external hostname (contains dot) gets global resolver",
			globalResolver: "udp://1.1.1.1:53",
			containers: []fakeContainer{
				{Names: []string{"/svc"}, Labels: map[string]string{
					"gatus.io/url": "https://example.com/health",
				}},
			},
			wantLen: 1,
			check: func(t *testing.T, eps []map[string]interface{}) {
				cb, ok := eps[0]["client"].(map[string]interface{})
				if !ok {
					t.Fatal("expected client block for external hostname")
				}
				if cb["dns-resolver"] != "udp://1.1.1.1:53" {
					t.Errorf("dns-resolver=%q", cb["dns-resolver"])
				}
			},
		},
		{
			name:           "internal hostname (no dot) does NOT get resolver",
			globalResolver: "udp://1.1.1.1:53",
			containers: []fakeContainer{
				{Names: []string{"/svc"}, Labels: map[string]string{
					"gatus.io/url": "http://internalservice/health",
				}},
			},
			wantLen: 1,
			check: func(t *testing.T, eps []map[string]interface{}) {
				if _, ok := eps[0]["client"]; ok {
					t.Error("expected no client block for no-dot hostname")
				}
			},
		},
		{
			name:           "running container name matches hostname skips resolver",
			globalResolver: "udp://1.1.1.1:53",
			containers: []fakeContainer{
				{Names: []string{"/my.service"}, Labels: map[string]string{
					"gatus.io/url": "http://my.service/health",
				}},
			},
			wantLen: 1,
			check: func(t *testing.T, eps []map[string]interface{}) {
				if _, ok := eps[0]["client"]; ok {
					t.Error("expected no client block when hostname matches running container name")
				}
			},
		},
		{
			name:           "per-label dns-resolver overrides global resolver",
			globalResolver: "udp://1.1.1.1:53",
			containers: []fakeContainer{
				{Names: []string{"/svc"}, Labels: map[string]string{
					"gatus.io/url":          "https://example.com/health",
					"gatus.io/dns-resolver": "udp://8.8.8.8:53",
				}},
			},
			wantLen: 1,
			check: func(t *testing.T, eps []map[string]interface{}) {
				cb, ok := eps[0]["client"].(map[string]interface{})
				if !ok {
					t.Fatal("expected client block")
				}
				if cb["dns-resolver"] != "udp://8.8.8.8:53" {
					t.Errorf("dns-resolver=%q, want per-label value", cb["dns-resolver"])
				}
			},
		},
		{
			name: "multiple containers produce separate endpoints",
			containers: []fakeContainer{
				{Names: []string{"/a"}, Labels: map[string]string{"gatus.io/url": "http://a/health"}},
				{Names: []string{"/b"}, Labels: map[string]string{"gatus.io/url": "http://b/health"}},
			},
			wantLen: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eps := syntheticDiscover(tc.containers, tc.globalResolver, tc.defaultInterval)
			if len(eps) != tc.wantLen {
				t.Fatalf("got %d endpoints, want %d; eps=%v", len(eps), tc.wantLen, eps)
			}
			if tc.check != nil {
				tc.check(t, eps)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 6. Alerting injection
// ---------------------------------------------------------------------------

func cloneEps(in []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, len(in))
	for i, ep := range in {
		cp := make(map[string]interface{})
		for k, v := range ep {
			cp[k] = v
		}
		out[i] = cp
	}
	return out
}

func applyAlertingInjection(cfg map[string]interface{}, eps []map[string]interface{}) []map[string]interface{} {
	providers := alertProviderList(cfg)
	if len(providers) > 0 {
		for i, ep := range eps {
			if _, has := ep["alerts"]; !has {
				ep["alerts"] = providers
				eps[i] = ep
			}
		}
	}
	return eps
}

func TestAlertingInjection(t *testing.T) {
	tests := []struct {
		name      string
		cfg       map[string]interface{}
		endpoints []map[string]interface{}
		check     func(t *testing.T, eps []map[string]interface{})
	}{
		{
			name: "no alerting config no injection",
			cfg:  map[string]interface{}{},
			endpoints: []map[string]interface{}{
				{"name": "ep1", "url": "http://x/h"},
			},
			check: func(t *testing.T, eps []map[string]interface{}) {
				if _, ok := eps[0]["alerts"]; ok {
					t.Error("expected no alerts injected when alerting not configured")
				}
			},
		},
		{
			name: "alerting configured injected into endpoint without alerts",
			cfg: map[string]interface{}{
				"alerting": map[string]interface{}{
					"slack": map[string]interface{}{},
				},
			},
			endpoints: []map[string]interface{}{
				{"name": "ep1", "url": "http://x/h"},
			},
			check: func(t *testing.T, eps []map[string]interface{}) {
				if _, ok := eps[0]["alerts"]; !ok {
					t.Error("expected alerts injected")
				}
			},
		},
		{
			name: "endpoint with existing alerts keeps its own alerts unchanged",
			cfg: map[string]interface{}{
				"alerting": map[string]interface{}{
					"slack": map[string]interface{}{},
				},
			},
			endpoints: []map[string]interface{}{
				{"name": "ep1", "url": "http://x/h"},
				{"name": "ep2", "url": "http://y/h", "alerts": []interface{}{}},
			},
			check: func(t *testing.T, eps []map[string]interface{}) {
				if _, ok := eps[0]["alerts"]; !ok {
					t.Error("expected alerts injected into ep1")
				}
				al, _ := eps[1]["alerts"].([]interface{})
				if len(al) != 0 {
					t.Errorf("ep2 alerts should remain empty slice, got %v", al)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := applyAlertingInjection(tc.cfg, cloneEps(tc.endpoints))
			tc.check(t, got)
		})
	}
}

// ---------------------------------------------------------------------------
// 7. DNS resolver injection on manual endpoints
// ---------------------------------------------------------------------------

func applyResolverInjection(eps []map[string]interface{}, globalResolver string) []map[string]interface{} {
	if globalResolver == "" {
		return eps
	}
	for i, ep := range eps {
		if _, hasClient := ep["client"]; hasClient {
			continue
		}
		u, _ := ep["url"].(string)
		host := hostnameFromURL(u)
		if hasDot(host) {
			ep["client"] = map[string]interface{}{
				"dns-resolver": globalResolver,
			}
			eps[i] = ep
		}
	}
	return eps
}

func TestResolverInjection(t *testing.T) {
	tests := []struct {
		name           string
		endpoints      []map[string]interface{}
		globalResolver string
		idx            int
		wantClient     bool
		wantResolver   string
	}{
		{
			name:           "no global resolver no injection",
			globalResolver: "",
			endpoints:      []map[string]interface{}{{"url": "https://example.com/h"}},
			idx:            0,
			wantClient:     false,
		},
		{
			name:           "external hostname gets resolver",
			globalResolver: "udp://1.1.1.1:53",
			endpoints:      []map[string]interface{}{{"url": "https://example.com/h"}},
			idx:            0,
			wantClient:     true,
			wantResolver:   "udp://1.1.1.1:53",
		},
		{
			name:           "internal hostname no dot skipped",
			globalResolver: "udp://1.1.1.1:53",
			endpoints:      []map[string]interface{}{{"url": "http://myservice/h"}},
			idx:            0,
			wantClient:     false,
		},
		{
			name:           "endpoint with existing client block not overwritten",
			globalResolver: "udp://1.1.1.1:53",
			endpoints: []map[string]interface{}{{
				"url":    "https://example.com/h",
				"client": map[string]interface{}{"dns-resolver": "udp://9.9.9.9:53"},
			}},
			idx:          0,
			wantClient:   true,
			wantResolver: "udp://9.9.9.9:53",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := applyResolverInjection(cloneEps(tc.endpoints), tc.globalResolver)
			ep := got[tc.idx]
			if !tc.wantClient {
				if _, ok := ep["client"]; ok {
					t.Errorf("expected no client block, got %v", ep["client"])
				}
				return
			}
			cb, ok := ep["client"].(map[string]interface{})
			if !ok {
				t.Fatal("expected client block to be a map")
			}
			if cb["dns-resolver"] != tc.wantResolver {
				t.Errorf("dns-resolver=%q, want %q", cb["dns-resolver"], tc.wantResolver)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 8. Fallback config appended when zero endpoints
// ---------------------------------------------------------------------------

func overridePaths(t *testing.T, defaults, fallback, merged, overrides string) {
	t.Helper()
	origDefaults, origFallback, origMerged, origOverrides :=
		defaultsPath, fallbackPath, mergedPath, overridesPath
	defaultsPath = defaults
	fallbackPath = fallback
	mergedPath = merged
	overridesPath = overrides
	t.Cleanup(func() {
		defaultsPath = origDefaults
		fallbackPath = origFallback
		mergedPath = origMerged
		overridesPath = origOverrides
	})
}

func readMergedYAML(t *testing.T, path string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read merged: %v", err)
	}
	var m map[string]interface{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal merged: %v", err)
	}
	return m
}

func TestFallbackAppended(t *testing.T) {
	dir := t.TempDir()
	mergedOut := filepath.Join(dir, "merged.yaml")

	mustWriteRaw(t, filepath.Join(dir, "defaults.yaml"), "server:\n  port: 8080\n")
	mustWriteRaw(t, filepath.Join(dir, "fallback.yaml"), `
endpoints:
  - name: self
    url: http://localhost:8080/health
    conditions:
      - "[STATUS] == 200"
`)
	overridePaths(t,
		filepath.Join(dir, "defaults.yaml"),
		filepath.Join(dir, "fallback.yaml"),
		mergedOut,
		filepath.Join(dir, "no-overrides.yaml"),
	)

	if err := generateConfig(nil); err != nil {
		t.Fatalf("generateConfig: %v", err)
	}

	merged := readMergedYAML(t, mergedOut)
	eps, ok := toSliceOfMaps(merged["endpoints"])
	if !ok || len(eps) == 0 {
		t.Fatal("expected fallback endpoints in merged config")
	}
	if eps[0]["name"] != "self" {
		t.Errorf("fallback endpoint name=%q, want self", eps[0]["name"])
	}
}

// ---------------------------------------------------------------------------
// 9. Wrapper-only keys consumed but logic driven correctly
// ---------------------------------------------------------------------------

func TestWrapperOnlyKeys_InternalHostnameNoResolver(t *testing.T) {
	dir := t.TempDir()
	mergedOut := filepath.Join(dir, "merged.yaml")

	mustWriteRaw(t, filepath.Join(dir, "defaults.yaml"), `
server:
  port: 8080
client:
  dns-resolver: "udp://1.1.1.1:53"
default:
  endpoints:
    interval: "2m"
endpoints:
  - name: manual
    url: http://internalservice/health
    conditions:
      - "[STATUS] == 200"
`)
	overridePaths(t,
		filepath.Join(dir, "defaults.yaml"),
		filepath.Join(dir, "no-fallback.yaml"),
		mergedOut,
		filepath.Join(dir, "no-overrides.yaml"),
	)

	if err := generateConfig(nil); err != nil {
		t.Fatalf("generateConfig: %v", err)
	}

	merged := readMergedYAML(t, mergedOut)
	eps, ok := toSliceOfMaps(merged["endpoints"])
	if !ok || len(eps) == 0 {
		t.Fatal("expected endpoints")
	}
	// Internal hostname has no dot → resolver must NOT be injected.
	if _, ok := eps[0]["client"]; ok {
		t.Error("internal endpoint should not have client/dns-resolver injected")
	}
}

func TestWrapperOnlyKeys_ExternalHostnameGetsResolver(t *testing.T) {
	dir := t.TempDir()
	mergedOut := filepath.Join(dir, "merged.yaml")

	mustWriteRaw(t, filepath.Join(dir, "defaults.yaml"), `
server:
  port: 8080
client:
  dns-resolver: "udp://1.1.1.1:53"
endpoints:
  - name: external
    url: https://example.com/health
    conditions:
      - "[STATUS] == 200"
`)
	overridePaths(t,
		filepath.Join(dir, "defaults.yaml"),
		filepath.Join(dir, "no-fallback.yaml"),
		mergedOut,
		filepath.Join(dir, "no-overrides.yaml"),
	)

	if err := generateConfig(nil); err != nil {
		t.Fatalf("generateConfig: %v", err)
	}

	merged := readMergedYAML(t, mergedOut)
	eps, ok := toSliceOfMaps(merged["endpoints"])
	if !ok || len(eps) == 0 {
		t.Fatal("expected endpoints")
	}
	cb, ok := eps[0]["client"].(map[string]interface{})
	if !ok {
		t.Fatal("expected client block injected for external hostname")
	}
	if cb["dns-resolver"] != "udp://1.1.1.1:53" {
		t.Errorf("dns-resolver=%q", cb["dns-resolver"])
	}
}

// ---------------------------------------------------------------------------
// 10. Deep-merge defaults + overrides integration (via generateConfig)
// ---------------------------------------------------------------------------

func TestDeepMergeDefaultsAndOverridesIntegration(t *testing.T) {
	dir := t.TempDir()
	mergedOut := filepath.Join(dir, "merged.yaml")

	mustWriteRaw(t, filepath.Join(dir, "defaults.yaml"), `
server:
  port: 8080
alerting:
  slack:
    webhook-url: https://hooks.slack.com/original
`)
	mustWriteRaw(t, filepath.Join(dir, "overrides.yaml"), `
server:
  port: 9090
alerting:
  slack:
    webhook-url: https://hooks.slack.com/override
endpoints:
  - name: override-ep
    url: http://override/health
    conditions:
      - "[STATUS] == 200"
`)
	overridePaths(t,
		filepath.Join(dir, "defaults.yaml"),
		filepath.Join(dir, "no-fallback.yaml"),
		mergedOut,
		filepath.Join(dir, "overrides.yaml"),
	)

	if err := generateConfig(nil); err != nil {
		t.Fatalf("generateConfig: %v", err)
	}

	merged := readMergedYAML(t, mergedOut)

	server, _ := merged["server"].(map[string]interface{})
	if server["port"] != 9090 {
		t.Errorf("server.port=%v, want 9090", server["port"])
	}

	alerting, _ := merged["alerting"].(map[string]interface{})
	slack, _ := alerting["slack"].(map[string]interface{})
	if slack["webhook-url"] != "https://hooks.slack.com/override" {
		t.Errorf("slack webhook=%q, want override value", slack["webhook-url"])
	}

	eps, ok := toSliceOfMaps(merged["endpoints"])
	if !ok || len(eps) == 0 {
		t.Fatal("expected endpoints")
	}
	if _, hasAlerts := eps[0]["alerts"]; !hasAlerts {
		t.Error("expected alerts injected into endpoint when alerting is configured")
	}
}
