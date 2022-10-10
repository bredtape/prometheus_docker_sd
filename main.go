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
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

var (
	outputFile string

	logger log.Logger
)

const (
	ENV_PREFIX = "promethes_docker_sd"
)

func parseArgs() *moby.DockerSDConfig {
	fs := flag.NewFlagSetWithEnvPrefix(os.Args[0], strings.ToUpper(ENV_PREFIX), flag.PanicOnError)
	fs.Usage = func() {
		fs.PrintDefaults()
	}

	var dockerHost, instancePrefix, targetNetworkName string
	var refreshInterval time.Duration
	flag.StringVar(&outputFile, "output-file", "docker_sd.json", "Output .json file with format as specified in https://prometheus.io/docs/prometheus/latest/configuration/configuration/#file_sd_config")
	flag.StringVar(&dockerHost, "docker-host", "unix:///var/run/docker.sock", "Docker host url")
	flag.StringVar(&targetNetworkName, "target-network-name", "metrics-net", "Network that the containers must be a member of to be considered. Consider making it 'external' in the docker-compose...")
	flag.StringVar(&instancePrefix, "instance-prefix", "", "Prefix added to Container name to form the 'instance' label. Required")
	flag.DurationVar(&refreshInterval, "refresh-interval", 5*time.Second, "Refresh interval to query the Docker host for containers")
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

	if len(strings.TrimSpace(targetNetworkName)) == 0 {
		_ = logger.Log("invalid target network name", "target-network-name", targetNetworkName)
		os.Exit(3)
	}

	return &moby.DockerSDConfig{
		Host:            dockerHost,
		InstancePrefix:  instancePrefix,
		TargetNetwork:   targetNetworkName,
		RefreshInterval: refreshInterval}
}

func main() {
	ctx := context.Background()
	config := parseArgs()

	logger := log.NewJSONLogger(os.Stdout)
	logger = log.With(logger, "ts", log.DefaultTimestampUTC)

	d, err := moby.NewDockerDiscovery(config, logger)
	if err != nil {
		_ = level.Error(logger).Log("failed to configure discovery", err)
		os.Exit(3)
	}

	t := time.After(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t:
			// refresh timer
			t = time.After(config.RefreshInterval)

			xs, err := d.Refresh(ctx)
			if err != nil {
				_ = level.Error(logger).Log("msg", "failed to refresh containers", "error", err)
			} else {
				err := writeResultsToFile(outputFile, xs)
				if err != nil {
					_ = level.Error(logger).Log("msg", "failed to write results", "error", err)
				}
			}
		}
	}
}

func writeResultsToFile(outputFile string, xs []moby.Export) error {
	data, err := yaml.Marshal(xs)
	if err != nil {
		return errors.Wrap(err, "failed to marshal")
	}
	return os.WriteFile(outputFile, data, 0644)
}
