package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bredtape/prometheus_docker_sd/docker"
	"github.com/bredtape/prometheus_docker_sd/web"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/namsral/flag"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

var (
	outputFile, httpAddress, externalUrl string

	logger log.Logger
)

func parseArgs() (*docker.Config, log.Logger) {
	fs := flag.NewFlagSetWithEnvPrefix(os.Args[0], strings.ToUpper(APP), flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s <options>\n", os.Args[0])
		fs.PrintDefaults()
		fmt.Fprintf(os.Stderr, "Options may also be set from the environment. Prefix with %s_, use all caps and replace any - with _\n", strings.ToUpper(APP))
	}

	var dockerHost, instancePrefix, targetNetworkName string
	var refreshInterval time.Duration
	fs.StringVar(&outputFile, "output-file", "docker_sd.yml", "Output .json, .yml or .yaml file with format as specified in https://prometheus.io/docs/prometheus/latest/configuration/configuration/#file_sd_config")
	fs.StringVar(&dockerHost, "docker-host", "unix:///var/run/docker.sock", "Docker host URL. Only socket have been tested.")
	fs.StringVar(&targetNetworkName, "target-network-name", "metrics-net", "Network that the containers must be a member of to be considered. Consider making it 'external' in the docker-compose...")
	fs.StringVar(&instancePrefix, "instance-prefix", "", "Prefix added to Container name to form the 'instance' label. Required")
	fs.DurationVar(&refreshInterval, "refresh-interval", 60*time.Second, "Refresh interval to query the Docker host for containers")
	fs.StringVar(&httpAddress, "http-address", ":9200", "http address to serve metrics on")
	fs.StringVar(&externalUrl, "external-url", "", "External URL of this service, defaults to http://<instance-prefix>:9200. Added to metrics label, so an alert can redirect a user to the /containers page")
	var logLevel string
	fs.StringVar(&logLevel, "log-level", "INFO", "Specify log level (DEBUG, INFO, WARN, ERROR)")

	var help bool
	fs.BoolVar(&help, "help", false, "Display help")

	err := fs.Parse(os.Args[1:])
	if err != nil {
		bail(fs, "failed to parse args: %v", err)
	}

	if help {
		fs.Usage()
		os.Exit(2)
	}

	if len(targetNetworkName) == 0 {
		bail(fs, "'target-network-name' required")
	}

	if len(instancePrefix) == 0 {
		bail(fs, "'instance-prefix' required")
	}

	lvl, err := level.Parse(strings.ToLower(logLevel))
	if err != nil {
		bail(fs, "'log-level' invalid: %v", err)
	}

	if externalUrl == "" {
		externalUrl = "http://" + instancePrefix + ":9200"
	}

	logger = log.NewLogfmtLogger(os.Stdout)
	logger = level.NewFilter(logger, level.Allow(lvl))
	logger = log.With(logger, "ts", log.DefaultTimestampUTC)

	return &docker.Config{
		Host:            dockerHost,
		InstancePrefix:  instancePrefix,
		TargetNetwork:   targetNetworkName,
		RefreshInterval: refreshInterval}, logger
}

func main() {
	ctx := context.Background()
	config, logger := parseArgs()

	_ = level.Info(logger).Log("msg", "starting http handler", "address", httpAddress)
	updates := make(chan []docker.Meta, 1)
	go web.Serve(httpAddress, updates)

	d, err := docker.New(config, logger)
	if err != nil {
		_ = level.Error(logger).Log("failed to configure discovery", err)
		os.Exit(4)
	}

	// init metrics
	mAttempts := metric_attempts.WithLabelValues(externalUrl, config.TargetNetwork)
	mErrors := metric_errors.WithLabelValues(externalUrl, config.TargetNetwork)

	t := time.After(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t:
			mAttempts.Inc()

			// refresh timer
			t = time.After(config.RefreshInterval)

			_ = level.Debug(logger).Log("msg", "begin refresh")
			xs, err := d.Refresh(ctx)
			if err != nil {
				mErrors.Inc()
				_ = level.Error(logger).Log("msg", "failed to refresh containers", "error", err)
				continue
			}

			err = writeResultsToFile(outputFile, convert(xs))
			if err != nil {
				mErrors.Inc()
				_ = level.Error(logger).Log("msg", "failed to write results", "error", err)
				continue
			}
			updateMetrics(externalUrl, config.TargetNetwork, xs)
			updates <- xs
			_ = level.Debug(logger).Log("msg", "done refresh")
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
