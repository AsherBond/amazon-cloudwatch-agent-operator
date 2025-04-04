// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	gokitlog "github.com/go-kit/log"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/discovery"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/aws/amazon-cloudwatch-agent-operator/cmd/amazon-cloudwatch-agent-target-allocator/allocation"
	"github.com/aws/amazon-cloudwatch-agent-operator/cmd/amazon-cloudwatch-agent-target-allocator/collector"
	"github.com/aws/amazon-cloudwatch-agent-operator/cmd/amazon-cloudwatch-agent-target-allocator/config"
	"github.com/aws/amazon-cloudwatch-agent-operator/cmd/amazon-cloudwatch-agent-target-allocator/prehook"
	"github.com/aws/amazon-cloudwatch-agent-operator/cmd/amazon-cloudwatch-agent-target-allocator/server"
	"github.com/aws/amazon-cloudwatch-agent-operator/cmd/amazon-cloudwatch-agent-target-allocator/target"
	allocatorWatcher "github.com/aws/amazon-cloudwatch-agent-operator/cmd/amazon-cloudwatch-agent-target-allocator/watcher"
)

var (
	setupLog     = ctrl.Log.WithName("setup")
	eventsMetric = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "cloudwatch_agent_allocator_events",
		Help: "Number of events in the channel.",
	}, []string{"source"})
)

func main() {
	var (
		// allocatorPrehook will be nil if filterStrategy is not set or
		// unrecognized. No filtering will be used in this case.
		allocatorPrehook prehook.Hook
		allocator        allocation.Allocator
		discoveryManager *discovery.Manager
		collectorWatcher *collector.Client
		fileWatcher      allocatorWatcher.Watcher
		promWatcher      allocatorWatcher.Watcher
		targetDiscoverer *target.Discoverer

		discoveryCancel context.CancelFunc
		runGroup        run.Group
		eventChan       = make(chan allocatorWatcher.Event)
		eventCloser     = make(chan bool, 1)
		interrupts      = make(chan os.Signal, 1)
		errChan         = make(chan error)
	)
	cfg, configFilePath, err := config.Load()

	if err != nil {
		fmt.Printf("Failed to load config: %v", err)
		os.Exit(1)
	}
	setupLog.Info("init config", "Config-Http", cfg.HTTPS)
	ctrl.SetLogger(cfg.RootLogger)

	if validationErr := config.ValidateConfig(cfg); validationErr != nil {
		setupLog.Error(validationErr, "Invalid configuration")
		os.Exit(1)
	}

	cfg.RootLogger.Info("Starting the Target Allocator")
	ctx := context.Background()
	log := ctrl.Log.WithName("allocator")

	allocatorPrehook = prehook.New(cfg.GetTargetsFilterStrategy(), log)
	allocator, err = allocation.New(cfg.GetAllocationStrategy(), log, allocation.WithFilter(allocatorPrehook))
	if err != nil {
		setupLog.Error(err, "Unable to initialize allocation strategy")
		os.Exit(1)
	}

	httpOptions := []server.Option{}
	tlsConfig, confErr := cfg.HTTPS.NewTLSConfig(ctx)
	if confErr != nil {
		setupLog.Error(confErr, "Unable to initialize TLS configuration", "Config", cfg.HTTPS)
		os.Exit(1)
	}
	httpOptions = append(httpOptions, server.WithTLSConfig(tlsConfig, cfg.HTTPS.ListenAddr))
	srv := server.NewServer(log, allocator, cfg.ListenAddr, httpOptions...)

	discoveryCtx, discoveryCancel := context.WithCancel(ctx)
	discoveryManager = discovery.NewManager(discoveryCtx, gokitlog.NewNopLogger())
	discovery.RegisterMetrics() // discovery manager metrics need to be enabled explicitly

	targetDiscoverer = target.NewDiscoverer(log, discoveryManager, allocatorPrehook, srv)
	collectorWatcher, collectorWatcherErr := collector.NewClient(log, cfg.ClusterConfig)
	if collectorWatcherErr != nil {
		setupLog.Error(collectorWatcherErr, "Unable to initialize collector watcher")
		os.Exit(1)
	}
	if cfg.ReloadConfig {
		fileWatcher, err = allocatorWatcher.NewFileWatcher(setupLog.WithName("file-watcher"), configFilePath)
		if err != nil {
			setupLog.Error(err, "Can't start the file watcher")
			os.Exit(1)
		}
	}
	signal.Notify(interrupts, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
	defer close(interrupts)

	if cfg.PrometheusCR.Enabled {
		promWatcher, err = allocatorWatcher.NewPrometheusCRWatcher(setupLog.WithName("prometheus-cr-watcher"), *cfg)
		if err != nil {
			setupLog.Error(err, "Can't start the prometheus watcher")
			os.Exit(1)
		}
		runGroup.Add(
			func() error {
				promWatcherErr := promWatcher.Watch(eventChan, errChan)
				setupLog.Info("Prometheus watcher exited")
				return promWatcherErr
			},
			func(_ error) {
				setupLog.Info("Closing prometheus watcher")
				promWatcherErr := promWatcher.Close()
				if promWatcherErr != nil {
					setupLog.Error(promWatcherErr, "prometheus watcher failed to close")
				}
			})
	}
	if cfg.ReloadConfig {
		runGroup.Add(
			func() error {
				fileWatcherErr := fileWatcher.Watch(eventChan, errChan)
				setupLog.Info("File watcher exited")
				return fileWatcherErr
			},
			func(_ error) {
				setupLog.Info("Closing file watcher")
				fileWatcherErr := fileWatcher.Close()
				if fileWatcherErr != nil {
					setupLog.Error(fileWatcherErr, "file watcher failed to close")
				}
			})
	}
	runGroup.Add(
		func() error {
			discoveryManagerErr := discoveryManager.Run()
			setupLog.Info("Discovery manager exited")
			return discoveryManagerErr
		},
		func(_ error) {
			setupLog.Info("Closing discovery manager")
			discoveryCancel()
		})
	runGroup.Add(
		func() error {
			// Initial loading of the config file's scrape config
			err = targetDiscoverer.ApplyConfig(allocatorWatcher.EventSourceConfigMap, cfg.PromConfig)
			if err != nil {
				setupLog.Error(err, "Unable to apply initial configuration")
				return err
			}
			err := targetDiscoverer.Watch(allocator.SetTargets)
			setupLog.Info("Target discoverer exited")
			return err
		},
		func(_ error) {
			setupLog.Info("Closing target discoverer")
			targetDiscoverer.Close()
		})
	runGroup.Add(
		func() error {
			err := collectorWatcher.Watch(ctx, cfg.LabelSelector, allocator.SetCollectors)
			setupLog.Info("Collector watcher exited")
			return err
		},
		func(_ error) {
			setupLog.Info("Closing collector watcher")
			collectorWatcher.Close()
		})
	runGroup.Add(
		func() error {
			err := srv.StartHTTPS()
			setupLog.Info("HTTPS Server failed to start", "error", err)
			return err
		},
		func(intrpError error) {
			setupLog.Info("Closing HTTPS server", "intrp", intrpError)
			if shutdownErr := srv.ShutdownHTTPS(ctx); shutdownErr != nil {
				setupLog.Error(shutdownErr, "Error on HTTPS server shutdown")
			}
		})
	runGroup.Add(
		func() error {
			for {
				select {
				case event := <-eventChan:
					eventsMetric.WithLabelValues(event.Source.String()).Inc()
					loadConfig, err := event.Watcher.LoadConfig(ctx)
					if err != nil {
						setupLog.Error(err, "Unable to load configuration")
						continue
					}
					err = targetDiscoverer.ApplyConfig(event.Source, loadConfig)
					if err != nil {
						setupLog.Error(err, "Unable to apply configuration")
						continue
					}
				case err := <-errChan:
					setupLog.Error(err, "Watcher error")
				case <-eventCloser:
					return nil
				}
			}
		},
		func(_ error) {
			setupLog.Info("Closing watcher loop")
			close(eventCloser)
		})
	runGroup.Add(
		func() error {
			for {
				select {
				case <-interrupts:
					setupLog.Info("Received interrupt")
					return nil
				case <-eventCloser:
					return nil
				}
			}
		},
		func(_ error) {
			setupLog.Info("Closing interrupt loop")
		})
	if runErr := runGroup.Run(); runErr != nil {
		setupLog.Error(runErr, "run group exited")
	}
	setupLog.Info("Target allocator exited.")
}
