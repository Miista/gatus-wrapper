package main

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"gopkg.in/yaml.v3"
)

var (
	defaultsPath  = envOr("DEFAULTS_PATH", "/etc/gatus/config.yaml")
	overridesPath = envOr("OVERRIDES_PATH", "/config/config.yaml")
	mergedPath    = envOr("MERGED_PATH", "/tmp/config.yaml")
	fallbackPath  = envOr("FALLBACK_PATH", "/etc/gatus/fallback.yaml")
	gatusBin      = envOr("GATUS_BIN", "/gatus")
	dockerSocket  = envOr("DOCKER_SOCKET", "/var/run/docker.sock")
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// deepMerge recursively merges override into base. Override values win.
// Arrays are replaced, not appended.
func deepMerge(base, override map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(base))
	for k, v := range base {
		result[k] = v
	}
	for k, v := range override {
		if vMap, ok := v.(map[string]interface{}); ok {
			if bMap, ok := result[k].(map[string]interface{}); ok {
				result[k] = deepMerge(bMap, vMap)
				continue
			}
		}
		result[k] = v
	}
	return result
}

func readYAML(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]interface{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = make(map[string]interface{})
	}
	return m, nil
}

func writeYAML(path string, m map[string]interface{}) error {
	data, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func hostnameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// runningContainerNames returns the set of container names (without leading /) for all running containers.
func runningContainerNames(cli *client.Client) (map[string]bool, error) {
	ctx := context.Background()
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: false})
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool)
	for _, c := range containers {
		for _, n := range c.Names {
			names[strings.TrimPrefix(n, "/")] = true
		}
	}
	return names, nil
}

// discoverEndpoints returns endpoints built from Docker labels.
func discoverEndpoints(cli *client.Client, globalResolver, defaultInterval string) []map[string]interface{} {
	containerNames, err := runningContainerNames(cli)
	if err != nil {
		slog.Warn("failed to list containers", "err", err)
		return nil
	}

	ctx := context.Background()
	containers, err := cli.ContainerList(ctx, container.ListOptions{All: false})
	if err != nil {
		slog.Warn("failed to list containers for label discovery", "err", err)
		return nil
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

		urls := strings.Fields(urlLabel)
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

		// Container name: strip leading /
		containerName := ""
		if len(c.Names) > 0 {
			containerName = strings.TrimPrefix(c.Names[0], "/")
		}

		for _, u := range urls {
			host := hostnameFromURL(u)

			var effectiveResolver string
			if labelResolver != "" {
				effectiveResolver = labelResolver
			} else if strings.Contains(host, ".") && !containerNames[host] && globalResolver != "" {
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

func generateConfig(cli *client.Client) error {
	// 1. Load defaults (required)
	cfg, err := readYAML(defaultsPath)
	if err != nil {
		return err
	}

	// 2. Deep-merge overrides if present
	if _, err := os.Stat(overridesPath); err == nil {
		overrides, err := readYAML(overridesPath)
		if err != nil {
			return err
		}
		cfg = deepMerge(cfg, overrides)
		slog.Info("merged overrides", "path", overridesPath)
	} else {
		slog.Info("no overrides, using defaults")
	}

	// 3. Extract wrapper-only keys
	globalResolver := ""
	if clientBlock, ok := cfg["client"].(map[string]interface{}); ok {
		if r, ok := clientBlock["dns-resolver"].(string); ok {
			globalResolver = r
		}
	}

	defaultInterval := ""
	if defaultBlock, ok := cfg["default"].(map[string]interface{}); ok {
		if epBlock, ok := defaultBlock["endpoints"].(map[string]interface{}); ok {
			if iv, ok := epBlock["interval"].(string); ok {
				defaultInterval = iv
			}
		}
	}

	// 4. Discover label endpoints
	var labelEndpoints []map[string]interface{}
	if cli != nil {
		labelEndpoints = discoverEndpoints(cli, globalResolver, defaultInterval)
	}

	// 5. Collect manual endpoints from merged config
	var manualEndpoints []map[string]interface{}
	if raw, ok := cfg["endpoints"]; ok {
		if list, ok := toSliceOfMaps(raw); ok {
			manualEndpoints = list
		}
	}

	allEndpoints := append(manualEndpoints, labelEndpoints...)

	// 6. Fallback if no endpoints at all
	if len(allEndpoints) == 0 {
		slog.Info("no endpoints found, appending fallback")
		fallback, err := os.ReadFile(fallbackPath)
		if err != nil {
			slog.Warn("could not read fallback", "err", err)
		} else {
			var fbMap map[string]interface{}
			if err := yaml.Unmarshal(fallback, &fbMap); err == nil && fbMap != nil {
				cfg = deepMerge(cfg, fbMap)
				if raw, ok := cfg["endpoints"]; ok {
					if list, ok := toSliceOfMaps(raw); ok {
						allEndpoints = list
					}
				}
			}
		}
	} else {
		cfg["endpoints"] = allEndpoints
	}

	// 7. Auto-inject alerting providers into endpoints missing alerts
	alertingProviders := alertProviderList(cfg)
	if len(alertingProviders) > 0 {
		for i, ep := range allEndpoints {
			if _, hasAlerts := ep["alerts"]; !hasAlerts {
				ep["alerts"] = alertingProviders
				allEndpoints[i] = ep
			}
		}
		cfg["endpoints"] = allEndpoints
	}

	// 8. Auto-inject client.dns-resolver for all endpoints missing a client block
	//    where hostname is external (contains ".") and global resolver is set
	if globalResolver != "" {
		for i, ep := range allEndpoints {
			if _, hasClient := ep["client"]; hasClient {
				continue
			}
			u, _ := ep["url"].(string)
			host := hostnameFromURL(u)
			if strings.Contains(host, ".") {
				ep["client"] = map[string]interface{}{
					"dns-resolver": globalResolver,
				}
				allEndpoints[i] = ep
			}
		}
		cfg["endpoints"] = allEndpoints
	}

	// 9. Write final config
	if err := writeYAML(mergedPath, cfg); err != nil {
		return err
	}
	slog.Info("config generated", "path", mergedPath,
		"endpoints", len(allEndpoints),
		"label_endpoints", len(labelEndpoints))
	return nil
}

func alertProviderList(cfg map[string]interface{}) []map[string]interface{} {
	alerting, ok := cfg["alerting"].(map[string]interface{})
	if !ok {
		return nil
	}
	var providers []map[string]interface{}
	for k := range alerting {
		providers = append(providers, map[string]interface{}{"type": k})
	}
	return providers
}

func toSliceOfMaps(v interface{}) ([]map[string]interface{}, bool) {
	slice, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	result := make([]map[string]interface{}, 0, len(slice))
	for _, item := range slice {
		if m, ok := item.(map[string]interface{}); ok {
			result = append(result, m)
		}
	}
	return result, true
}

func newDockerClient() *client.Client {
	if _, err := os.Stat(dockerSocket); err != nil {
		slog.Warn("docker socket not available, skipping label discovery", "socket", dockerSocket)
		return nil
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		slog.Warn("failed to create docker client", "err", err)
		return nil
	}
	return cli
}

func watchDockerEvents(cli *client.Client, onChange func()) {
	for {
		f := filters.NewArgs()
		f.Add("type", string(events.ContainerEventType))
		f.Add("event", "start")
		f.Add("event", "die")

		ctx := context.Background()
		msgCh, errCh := cli.Events(ctx, events.ListOptions{Filters: f})

		done := false
		for !done {
			select {
			case msg := <-msgCh:
				slog.Info("container event, regenerating config",
					"action", msg.Action,
					"container", msg.Actor.Attributes["name"])
				onChange()
			case err := <-errCh:
				if err != nil && err != io.EOF {
					slog.Warn("docker event stream error, reconnecting", "err", err)
				}
				done = true
			}
		}

		time.Sleep(5 * time.Second)
	}
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	// Validate defaults exist
	if _, err := os.Stat(defaultsPath); err != nil {
		slog.Error("defaults config not found", "path", defaultsPath)
		os.Exit(1)
	}

	cli := newDockerClient()

	// Generate initial config
	if err := generateConfig(cli); err != nil {
		slog.Error("failed to generate config", "err", err)
		os.Exit(1)
	}

	// Launch gatus
	cmd := exec.Command(gatusBin)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		slog.Error("failed to start gatus", "err", err)
		os.Exit(1)
	}
	slog.Info("gatus started", "pid", cmd.Process.Pid)

	// Watch docker events in background
	if cli != nil {
		go watchDockerEvents(cli, func() {
			if err := generateConfig(cli); err != nil {
				slog.Warn("config regeneration failed", "err", err)
			}
		})
	}

	// Wait for gatus to exit
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			slog.Info("gatus exited", "code", exitErr.ExitCode())
			os.Exit(exitErr.ExitCode())
		}
		slog.Error("gatus exited with error", "err", err)
		os.Exit(1)
	}
	slog.Info("gatus exited cleanly")
}
