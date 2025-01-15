package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bredtape/prometheus_docker_sd/docker"
	"github.com/bredtape/prometheus_docker_sd/web"
	"github.com/bredtape/slogging"
	"github.com/peterbourgon/ff/v3"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"gopkg.in/yaml.v3"
)

const (
	APP = "prometheus_docker_sd"
)

var (
	outputFile, httpAddress, externalUrl string
)

func parseArgs() *docker.Config {
	envPrefix := strings.ToUpper(APP)
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fs.Usage = func() {
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "Options may also be set from the environment. Prefix with %s_, use all caps. and replace any - with _\n", envPrefix)
		os.Exit(1)
	}

	var dockerHost, instancePrefix, externalHost, targetNetworkName string
	var refreshInterval time.Duration
	fs.StringVar(&outputFile, "output-file", "docker_sd.yml", "Output .json, .yml or .yaml file with format as specified in https://prometheus.io/docs/prometheus/latest/configuration/configuration/#file_sd_config")
	fs.StringVar(&dockerHost, "docker-host", "unix:///var/run/docker.sock", "Docker host URL. Only socket have been tested.")
	fs.StringVar(&targetNetworkName, "target-network-name", "metrics-net", "Network that the containers must be a member of to be considered. Consider making it 'external' in the docker-compose...")
	fs.StringVar(&instancePrefix, "instance-prefix", "", "Prefix added to Container name to form the 'instance' label. Required")
	fs.StringVar(&externalHost, "external-host", "", "External host of this service, defaults to <instance-prefix>, when not specified. Used for external scrape targets")
	fs.DurationVar(&refreshInterval, "refresh-interval", 60*time.Second, "Refresh interval to query the Docker host for containers")
	fs.StringVar(&httpAddress, "http-address", ":9200", "http address to serve metrics on")
	fs.StringVar(&externalUrl, "external-url", "", "External URL of this service, defaults to http://<instance-prefix>:9200. Added to metrics label, so an alert can redirect a user to the /containers page")

	var logLevel slog.Level
	fs.TextVar(&logLevel, "log-level", slog.LevelDebug-3, "Log level")
	var logJSON bool
	fs.BoolVar(&logJSON, "log-json", false, "Log in JSON format")
	var help bool
	fs.BoolVar(&help, "help", false, "Show help")

	err := ff.Parse(fs, os.Args[1:], ff.WithEnvVarPrefix(envPrefix))
	if err != nil {
		bail(fs, "parse error %s", err.Error())
		os.Exit(2)
	}

	if help {
		fs.Usage()
		os.Exit(2)
	}

	slogging.SetDefaults(slog.HandlerOptions{Level: logLevel}, logJSON)
	slogging.LogBuildInfo()

	if len(targetNetworkName) == 0 {
		bail(fs, "'target-network-name' required")
	}

	if len(instancePrefix) == 0 {
		bail(fs, "'instance-prefix' required")
	}

	if externalUrl == "" {
		externalUrl = "http://" + instancePrefix + ":9200"
	}
	if externalHost == "" {
		externalHost = instancePrefix
	}

	return &docker.Config{
		DockerHost:      dockerHost,
		InstancePrefix:  instancePrefix,
		ExternalHost:    externalHost,
		TargetNetwork:   targetNetworkName,
		RefreshInterval: refreshInterval}
}

func main() {
	ctx := context.Background()
	config := parseArgs()
	log := slog.Default()

	updates := make(chan []docker.Meta, 1)
	log.Info("starting http handler", "address", httpAddress)
	go web.Serve(httpAddress, updates)

	d, err := docker.New(config)
	if err != nil {
		log.Error("failed to configure discovery", "error", err)
		os.Exit(4)
	}

	// init metrics
	mAttempts := metric_attempts.WithLabelValues(externalUrl, config.TargetNetwork)
	mErrors := metric_errors.WithLabelValues(externalUrl, config.TargetNetwork)

	t := time.After(0)
	log = log.With("context", "main")
	for {
		select {
		case <-ctx.Done():
			return
		case <-t:
			mAttempts.Inc()

			// refresh timer
			t = time.After(config.RefreshInterval)

			log.Info("begin refresh")
			xs, err := d.Refresh(ctx)
			if err != nil {
				mErrors.Inc()
				log.Error("failed to refresh containers", "error", err)
				continue
			}

			err = writeResultsToFile(outputFile, convert(xs))
			if err != nil {
				mErrors.Inc()
				log.Error("failed to write results", "error", err)
				continue
			}
			updateMetrics(externalUrl, config.TargetNetwork, xs)
			updates <- xs
			log.Debug("done refresh")
		}
	}
}

type Export struct {
	Targets []string          `yaml:"targets"`
	Labels  map[string]string `yaml:"labels"`
}

func writeResultsToFile(outputFile string, xs []Export) error {
	switch filepath.Ext(strings.ToLower(outputFile)) {
	case ".yml", ".yaml":
		data, err := yaml.Marshal(xs)
		if err != nil {
			return errors.Wrap(err, "failed to marshal")
		}
		return os.WriteFile(outputFile, data, 0644)
	case ".json":
		data, err := json.Marshal(xs)
		if err != nil {
			return errors.Wrap(err, "failed to marshal")
		}
		return os.WriteFile(outputFile, data, 0644)
	default:
		return fmt.Errorf("unsupported file extension in output-file: %s", outputFile)
	}
}

func convert(xs []docker.Meta) []Export {
	ys := make([]Export, 0)
	for _, x := range xs {
		if !x.IsExported() {
			continue
		}
		ys = append(ys, Export{
			Targets: []string{x.Address},
			Labels:  x.Labels})
	}
	return ys
}

func bail(fs *flag.FlagSet, format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
	fs.Usage()
	os.Exit(3)
}

var (
	labelKeys = []string{"external_url", "target_network"}

	metric_attempts = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: APP,
		Name:      "discovery_attempts_total",
		Help:      "Number of attempts to discover containers and write result"}, labelKeys)

	metric_errors = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: APP,
		Name:      "discovery_attempts_errors_total",
		Help:      "Number of attempts to discover containers and write result, that resulted in some error"}, labelKeys)

	metric_count = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: APP,
		Name:      "containers_count",
		Help:      "Number of containers discovered"},
		labelKeys)

	metric_ignored = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: APP,
		Name:      "containers_ignored_count",
		Help:      "Number of containers discovered that were ignored"},
		labelKeys)

	metric_ignored_containers_not_in_network = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: APP,
		Name:      "containers_not_in_target_network_count",
		Help:      "Number of containers discovered with the 'prometheus_job' label set, but not in the target network"},
		labelKeys)

	metric_ignored_no_ports = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: APP,
		Name:      "containers_no_exposed_ports_count",
		Help:      "Number of containers discovered with the 'prometheus_job' label set, but with no exposed TCP ports"},
		labelKeys)

	metric_multiple_ports = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: APP,
		Name:      "containers_multiple_ports_not_explicit_count",
		Help:      "Number of containers discovered with the 'prometheus_job' label set, with multiple exposed TCP ports, but the prometheus_scrape_port is not defined"},
		labelKeys)
)

func updateMetrics(externalUrl, targetNetwork string, xs []docker.Meta) {
	var ignored, notInNetwork, noPorts, notExplicit float64
	for _, x := range xs {
		if !x.HasJob {
			ignored++
			continue
		}

		if !x.IsInTargetNetwork {
			notInNetwork++
			continue
		}

		if !x.HasTCPPorts {
			noPorts++
			continue
		}

		if !x.HasExplicitPort {
			notExplicit++
		}
	}

	metric_count.WithLabelValues(externalUrl, targetNetwork).Set(float64(len(xs)))
	metric_ignored.WithLabelValues(externalUrl, targetNetwork).Set(ignored)
	metric_ignored_containers_not_in_network.WithLabelValues(externalUrl, targetNetwork).Set(notInNetwork)
	metric_ignored_no_ports.WithLabelValues(externalUrl, targetNetwork).Set(noPorts)
	metric_multiple_ports.WithLabelValues(externalUrl, targetNetwork).Set(notExplicit)
}
