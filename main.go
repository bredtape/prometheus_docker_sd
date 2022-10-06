package main

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/bredtape/prometheus_docker_sd/moby"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/namsral/flag"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/targetgroup"
)

var (
	outputFile, dockerHost, hostNetworkingHost     string
	refreshInterval, scrapeInterval, scrapeTimeout time.Duration
	logger                                         log.Logger
)

const (
	ENV_PREFIX = "promethes_docker_sd"
)

func parseArgs() {
	fs := flag.NewFlagSetWithEnvPrefix(os.Args[0], strings.ToUpper(ENV_PREFIX), flag.PanicOnError)
	fs.Usage = func() {
		fs.PrintDefaults()
	}

	flag.StringVar(&outputFile, "output-file", "docker_sd.json", "Output .json file with format as specified in https://prometheus.io/docs/prometheus/latest/configuration/configuration/#file_sd_config")
	flag.StringVar(&dockerHost, "docker-host", "unix:///var/run/docker.sock", "Docker host url")
	flag.DurationVar(&refreshInterval, "refresh-interval", 60*time.Second, "Refresh interval to query the Docker host for containers")
	flag.DurationVar(&scrapeInterval, "scrape-interval", 60*time.Second, "Default scrape interval")
	flag.DurationVar(&scrapeTimeout, "scrape-timeout", 10*time.Second, "Default scrape timeout")
	flag.StringVar(&hostNetworkingHost, "host-networking-host", "localhost", "The host to use if the container is in host networking mode")
	var logLevel string
	flag.StringVar(&logLevel, "log-level", "DEBUG", "Specify log level (DEBUG, INFO, WARN, ERROR)")

	var help bool
	flag.BoolVar(&help, "help", false, "Display help")
	flag.Parse()

	if help {
		fs.Usage()
		os.Exit(2)
	}

	logger = log.NewLogfmtLogger(os.Stderr)
	logger = level.NewFilter(logger, level.Allow(level.ParseDefault(logLevel, level.InfoValue())))
	logger = log.With(logger, "ts", log.DefaultTimestampUTC)
}

func main() {
	ctx := context.Background()
	parseArgs()

	logger := log.NewJSONLogger(os.Stdout)

	config := &moby.DockerSDConfig{
		Host:               dockerHost,
		Port:               80,
		HostNetworkingHost: hostNetworkingHost,
		RefreshInterval:    model.Duration(refreshInterval)}

	d, err := moby.NewDockerDiscovery(config, logger)
	if err != nil {
		_ = level.Error(logger).Log("failed to configure discovery", err)
		os.Exit(3)
	}

	ch := make(chan []*targetgroup.Group)
	_ = level.Debug(logger).Log("message", "Run")
	go d.Run(ctx, ch)

	for gs := range ch {
		_ = level.Debug(logger).Log("message", "--- new results ---")
		_ = level.Debug(logger).Log("list", gs)

		for _, x := range gs {
			_ = level.Debug(logger).Log("source", x.Source, "labels", x.Labels, "targets", x.Targets)
		}

		for _, x := range extract(gs) {
			_ = level.Debug(logger).Log("message", "after extract", "source", x.Source, "labels", x.Labels, "targets", x.Targets)
		}
	}
}

func extract(gs []*targetgroup.Group) []*targetgroup.Group {
	if len(gs) == 0 {
		return nil
	}

	g := gs[0]

	targets := make([]model.LabelSet, 0)

	for _, set := range g.Targets {
		target := make(model.LabelSet)

		for k, v := range set {
			if strings.HasPrefix(string(k), moby.ExtractLabelPrefix) {
				target[k[len(moby.ExtractLabelPrefix):]] = v
			} else {
				target[k] = v
			}
		}

		targets = append(targets, target)
	}

	return []*targetgroup.Group{{Targets: targets, Source: g.Source}}
}
