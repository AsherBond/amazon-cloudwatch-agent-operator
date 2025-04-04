// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	"context"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/aws/amazon-cloudwatch-agent-operator/cmd/amazon-cloudwatch-agent-target-allocator/allocation"
)

const (
	watcherTimeout = 15 * time.Minute
)

var (
	ns                   = os.Getenv("OTELCOL_NAMESPACE")
	collectorsDiscovered = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "amazon_cloudwatch_agent_allocator_collectors_discovered",
		Help: "Number of collectors discovered.",
	})
)

type Client struct {
	log       logr.Logger
	k8sClient kubernetes.Interface
	close     chan struct{}
}

func NewClient(logger logr.Logger, kubeConfig *rest.Config) (*Client, error) {
	clientset, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return &Client{}, err
	}

	return &Client{
		log:       logger.WithValues("component", "amazon-cloudwatch-agent-target-allocator"),
		k8sClient: clientset,
		close:     make(chan struct{}),
	}, nil
}

func (k *Client) Watch(ctx context.Context, labelMap map[string]string, fn func(collectors map[string]*allocation.Collector)) error {
	collectorMap := map[string]*allocation.Collector{}

	opts := metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labelMap).String(),
	}
	pods, err := k.k8sClient.CoreV1().Pods(ns).List(ctx, opts)
	if err != nil {
		k.log.Error(err, "Pod failure")
		os.Exit(1)
	}
	for i := range pods.Items {
		pod := pods.Items[i]
		if pod.GetObjectMeta().GetDeletionTimestamp() == nil {
			collectorMap[pod.Name] = allocation.NewCollector(pod.Name)
		}
	}
	fn(collectorMap)

	for {
		if !k.restartWatch(ctx, opts, collectorMap, fn) {
			return nil
		}
	}
}

func (k *Client) restartWatch(ctx context.Context, opts metav1.ListOptions, collectorMap map[string]*allocation.Collector, fn func(collectors map[string]*allocation.Collector)) bool {
	// add timeout to the context before calling Watch
	ctx, cancel := context.WithTimeout(ctx, watcherTimeout)
	defer cancel()
	watcher, err := k.k8sClient.CoreV1().Pods(ns).Watch(ctx, opts)
	if err != nil {
		k.log.Error(err, "unable to create collector pod watcher")
		return false
	}
	k.log.Info("Successfully started a collector pod watcher")
	if msg := runWatch(ctx, k, watcher.ResultChan(), collectorMap, fn); msg != "" {
		k.log.Info("Collector pod watch event stopped " + msg)
		return false
	}

	return true
}

func runWatch(ctx context.Context, k *Client, c <-chan watch.Event, collectorMap map[string]*allocation.Collector, fn func(collectors map[string]*allocation.Collector)) string {
	for {
		collectorsDiscovered.Set(float64(len(collectorMap)))
		select {
		case <-k.close:
			return "kubernetes client closed"
		case <-ctx.Done():
			return "" // this means that the watcher most likely timed out
		case event, ok := <-c:
			if !ok {
				k.log.Info("No event found. Restarting watch routine")
				return ""
			}

			pod, ok := event.Object.(*v1.Pod)
			if !ok {
				k.log.Info("No pod found in event Object. Restarting watch routine")
				return ""
			}

			switch event.Type { //nolint:exhaustive
			case watch.Added:
				collectorMap[pod.Name] = allocation.NewCollector(pod.Name)
			case watch.Deleted:
				delete(collectorMap, pod.Name)
			}
			fn(collectorMap)
		}
	}
}

func (k *Client) Close() {
	close(k.close)
}
