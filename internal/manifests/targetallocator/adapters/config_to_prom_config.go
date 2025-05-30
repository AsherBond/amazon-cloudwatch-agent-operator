// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package adapters

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"

	"github.com/aws/amazon-cloudwatch-agent-operator/internal/manifests/collector/adapters"
	"github.com/aws/amazon-cloudwatch-agent-operator/internal/naming"
)

func errorNoComponent(component string) error {
	return fmt.Errorf("no %s available as part of the configuration", component)
}

func errorNotAMapAtIndex(component string, index int) error {
	return fmt.Errorf("index %d: %s property in the configuration doesn't contain a valid map: %s", index, component, component)
}

func errorNotAMap(component string) error {
	return fmt.Errorf("%s property in the configuration doesn't contain valid %s", component, component)
}

func errorNotAList(component string) error {
	return fmt.Errorf("%s must be a list in the config", component)
}

func errorNotAListAtIndex(component string, index int) error {
	return fmt.Errorf("index %d: %s property in the configuration doesn't contain a valid index: %s", index, component, component)
}

func errorNotAStringAtIndex(component string, index int) error {
	return fmt.Errorf("index %d: %s property in the configuration doesn't contain a valid string: %s", index, component, component)
}

// getScrapeConfigsFromPromConfig extracts the scrapeConfig array from prometheus config.
func getScrapeConfigsFromPromConfig(promConfig map[interface{}]interface{}) ([]interface{}, error) {
	prometheusConfigProperty, ok := promConfig["config"]
	if !ok {
		return nil, errorNoComponent("prometheusConfig")
	}

	prometheusConfig, ok := prometheusConfigProperty.(map[interface{}]interface{})
	if !ok {
		return nil, errorNotAMap("prometheusConfig")
	}

	scrapeConfigsProperty, ok := prometheusConfig["scrape_configs"]
	if !ok {
		return nil, errorNoComponent("scrape_configs")
	}

	scrapeConfigs, ok := scrapeConfigsProperty.([]interface{})
	if !ok {
		return nil, errorNotAList("scrape_configs")
	}

	return scrapeConfigs, nil
}

// GetPromConfig returns a Prometheus configuration file.
func GetPromConfig(cfg string) (map[interface{}]interface{}, error) {
	prometheus, err := adapters.ConfigFromString(cfg)
	if err != nil {
		return nil, err
	}

	scrapeConfigs, err := getScrapeConfigsFromPromConfig(prometheus)
	if err != nil {
		return nil, err
	}

	for i, config := range scrapeConfigs {
		scrapeConfig, ok := config.(map[interface{}]interface{})
		if !ok {
			return nil, errorNotAMapAtIndex("scrape_config", i)
		}

		relabelConfigsProperty, ok := scrapeConfig["relabel_configs"]
		if !ok {
			continue
		}

		relabelConfigs, ok := relabelConfigsProperty.([]interface{})
		if !ok {
			return nil, errorNotAListAtIndex("relabel_configs", i)
		}

		for i, rc := range relabelConfigs {
			relabelConfig, rcErr := rc.(map[interface{}]interface{})
			if !rcErr {
				return nil, errorNotAMapAtIndex("relabel_config", i)
			}

			replacementProperty, rcErr := relabelConfig["replacement"]
			if !rcErr {
				continue
			}

			_, rcErr = replacementProperty.(string)
			if !rcErr {
				return nil, errorNotAStringAtIndex("replacement", i)
			}
		}

		metricRelabelConfigsProperty, ok := scrapeConfig["metric_relabel_configs"]
		if !ok {
			continue
		}

		metricRelabelConfigs, ok := metricRelabelConfigsProperty.([]interface{})
		if !ok {
			return nil, errorNotAListAtIndex("metric_relabel_configs", i)
		}

		for i, rc := range metricRelabelConfigs {
			relabelConfig, ok := rc.(map[interface{}]interface{})
			if !ok {
				return nil, errorNotAMapAtIndex("metric_relabel_config", i)
			}

			replacementProperty, ok := relabelConfig["replacement"]
			if !ok {
				continue
			}

			_, ok = replacementProperty.(string)
			if !ok {
				return nil, errorNotAStringAtIndex("replacement", i)
			}
		}
	}

	return prometheus, nil
}

// AddHTTPSDConfigToPromConfig adds HTTP SD (Service Discovery) configuration to the Prometheus configuration.
// This function removes any existing service discovery configurations (e.g., `sd_configs`, `dns_sd_configs`, `file_sd_configs`, etc.)
// from the `scrape_configs` section and adds a single `http_sd_configs` configuration.
// The `http_sd_configs` points to the TA (Target Allocator) endpoint that provides the list of targets for the given job.
func AddHTTPSDConfigToPromConfig(prometheus map[interface{}]interface{}, taServiceName string) (map[interface{}]interface{}, error) {
	prometheusConfigProperty, ok := prometheus["config"]
	if !ok {
		return nil, errorNoComponent("prometheusConfig")
	}

	prometheusConfig, ok := prometheusConfigProperty.(map[interface{}]interface{})
	if !ok {
		return nil, errorNotAMap("prometheusConfig")
	}

	scrapeConfigsProperty, ok := prometheusConfig["scrape_configs"]
	if !ok {
		return nil, errorNoComponent("scrape_configs")
	}

	scrapeConfigs, ok := scrapeConfigsProperty.([]interface{})
	if !ok {
		return nil, errorNotAList("scrape_configs")
	}

	sdRegex := regexp.MustCompile(`^.*(sd|static)_configs$`)

	for i, config := range scrapeConfigs {
		scrapeConfig, ok := config.(map[interface{}]interface{})
		if !ok {
			return nil, errorNotAMapAtIndex("scrape_config", i)
		}

		// Check for other types of service discovery configs (e.g. dns_sd_configs, file_sd_configs, etc.)
		for key := range scrapeConfig {
			keyStr, keyErr := key.(string)
			if !keyErr {
				continue
			}
			if sdRegex.MatchString(keyStr) {
				delete(scrapeConfig, key)
			}
		}

		jobNameProperty, ok := scrapeConfig["job_name"]
		if !ok {
			return nil, errorNotAStringAtIndex("job_name", i)
		}

		jobName, ok := jobNameProperty.(string)
		if !ok {
			return nil, errorNotAStringAtIndex("job_name is not a string", i)
		}

		escapedJob := url.QueryEscape(jobName)
		scrapeConfig["http_sd_configs"] = []interface{}{
			map[string]interface{}{
				"url": fmt.Sprintf("https://%s:%d/jobs/%s/targets", taServiceName, naming.TargetAllocatorServicePort, escapedJob),
			},
		}
	}

	return prometheus, nil
}

// AddTAConfigToPromConfig adds or updates the target_allocator configuration in the Prometheus configuration.
// If the `EnableTargetAllocatorRewrite` feature flag for the target allocator is enabled, this function
// removes the existing scrape_configs from the collector's Prometheus configuration as it's not required.
func AddTAConfigToPromConfig(prometheus map[interface{}]interface{}, taServiceName string) (map[interface{}]interface{}, error) {
	prometheusConfigProperty, ok := prometheus["config"]
	if !ok {
		return nil, errorNoComponent("prometheusConfig")
	}

	prometheusCfg, ok := prometheusConfigProperty.(map[interface{}]interface{})
	if !ok {
		return nil, errorNotAMap("prometheusConfig")
	}

	// Create the TargetAllocConfig dynamically if it doesn't exist
	if prometheus["target_allocator"] == nil {
		prometheus["target_allocator"] = make(map[interface{}]interface{})
	}

	targetAllocatorCfg, ok := prometheus["target_allocator"].(map[interface{}]interface{})
	if !ok {
		return nil, errorNotAMap("target_allocator")
	}

	targetAllocatorCfg["endpoint"] = fmt.Sprintf("https://%s:%d", taServiceName, naming.TargetAllocatorServicePort)
	targetAllocatorCfg["interval"] = "30s"

	// Remove the scrape_configs key from the map
	delete(prometheusCfg, "scrape_configs")

	return prometheus, nil
}

// ValidatePromConfig checks if the prometheus config is valid given other collector-level settings.
func ValidatePromConfig(config map[interface{}]interface{}, targetAllocatorEnabled bool, targetAllocatorRewriteEnabled bool) error {
	_, promConfigExists := config["config"]

	if targetAllocatorEnabled {
		if targetAllocatorRewriteEnabled { // if rewrite is enabled, we will add a target_allocator section during rewrite
			return nil
		}
		_, targetAllocatorExists := config["target_allocator"]

		// otherwise, either the target_allocator or config section needs to be here
		if !(promConfigExists || targetAllocatorExists) {
			return errors.New("either target allocator or prometheus config needs to be present")
		}

		return nil
	}
	// if target allocator isn't enabled, we need a config section
	if !promConfigExists {
		return errorNoComponent("prometheusConfig")
	}

	return nil
}

// ValidateTargetAllocatorConfig checks if the Target Allocator config is valid
// In order for Target Allocator to do anything useful, at least one of the following has to be true:
//   - at least one scrape config has to be defined in Prometheus configuration
//   - PrometheusCR has to be enabled in target allocator settings
func ValidateTargetAllocatorConfig(targetAllocatorPrometheusCR bool, promConfig map[interface{}]interface{}) error {

	if targetAllocatorPrometheusCR {
		return nil
	}
	// if PrometheusCR isn't enabled, we need at least one scrape config
	scrapeConfigs, err := getScrapeConfigsFromPromConfig(promConfig)
	if err != nil {
		return err
	}

	if len(scrapeConfigs) == 0 {
		return fmt.Errorf("either at least one scrape config needs to be defined or PrometheusCR needs to be enabled")
	}

	return nil
}
