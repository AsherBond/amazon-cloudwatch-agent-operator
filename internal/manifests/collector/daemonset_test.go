// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

//go:build ignore_test

package collector_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/aws/amazon-cloudwatch-agent-operator/apis/v1alpha1"
	"github.com/aws/amazon-cloudwatch-agent-operator/internal/config"
	"github.com/aws/amazon-cloudwatch-agent-operator/internal/manifests"
	. "github.com/aws/amazon-cloudwatch-agent-operator/internal/manifests/collector"
)

func TestDaemonSetNewDefault(t *testing.T) {
	// prepare
	params := manifests.Params{
		Config: config.New(),
		OtelCol: v1alpha1.AmazonCloudWatchAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-instance",
				Namespace: "my-namespace",
			},
			Spec: v1alpha1.AmazonCloudWatchAgentSpec{
				Tolerations: testTolerationValues,
			},
		},
		Log: logger,
	}

	// test
	d := DaemonSet(params)

	// verify
	assert.Equal(t, "my-instance", d.Name)
	assert.Equal(t, "my-instance", d.Labels["app.kubernetes.io/name"])
	assert.Equal(t, testTolerationValues, d.Spec.Template.Spec.Tolerations)

	assert.Len(t, d.Spec.Template.Spec.Containers, 1)

	// verify sha256 podAnnotation
	expectedAnnotations := map[string]string{
		"amazon-cloudwatch-agent-operator-config/sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}
	assert.Equal(t, expectedAnnotations, d.Spec.Template.Annotations)

	expectedLabels := map[string]string{
		"app.kubernetes.io/component":  "amazon-cloudwatch-agent",
		"app.kubernetes.io/instance":   "my-namespace.my-instance",
		"app.kubernetes.io/managed-by": "amazon-cloudwatch-agent-operator",
		"app.kubernetes.io/name":       "my-instance",
		"app.kubernetes.io/part-of":    "amazon-cloudwatch-agent",
		"app.kubernetes.io/version":    "latest",
	}
	assert.Equal(t, expectedLabels, d.Spec.Template.Labels)

	expectedSelectorLabels := map[string]string{
		"app.kubernetes.io/component":  "amazon-cloudwatch-agent",
		"app.kubernetes.io/instance":   "my-namespace.my-instance",
		"app.kubernetes.io/managed-by": "amazon-cloudwatch-agent-operator",
		"app.kubernetes.io/part-of":    "amazon-cloudwatch-agent",
	}
	assert.Equal(t, expectedSelectorLabels, d.Spec.Selector.MatchLabels)

	// the pod selector must be contained within pod spec's labels
	for k, v := range d.Spec.Selector.MatchLabels {
		assert.Equal(t, v, d.Spec.Template.Labels[k])
	}
}

func TestDaemonsetHostNetwork(t *testing.T) {
	params1 := manifests.Params{
		Config: config.New(),
		OtelCol: v1alpha1.AmazonCloudWatchAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-instance",
				Namespace: "my-namespace",
			},
			Spec: v1alpha1.AmazonCloudWatchAgentSpec{},
		},
		Log: logger,
	}
	// test
	d1 := DaemonSet(params1)
	assert.False(t, d1.Spec.Template.Spec.HostNetwork)
	assert.Equal(t, d1.Spec.Template.Spec.DNSPolicy, v1.DNSClusterFirst)

	// verify custom
	params2 := manifests.Params{
		Config: config.New(),
		OtelCol: v1alpha1.AmazonCloudWatchAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-instance",
				Namespace: "my-namespace",
			},
			Spec: v1alpha1.AmazonCloudWatchAgentSpec{
				HostNetwork: true,
			},
		},
		Log: logger,
	}
	d2 := DaemonSet(params2)
	assert.True(t, d2.Spec.Template.Spec.HostNetwork)
	assert.Equal(t, d2.Spec.Template.Spec.DNSPolicy, v1.DNSClusterFirstWithHostNet)
}

func TestDaemonsetPodAnnotations(t *testing.T) {
	// prepare
	testPodAnnotationValues := map[string]string{"annotation-key": "annotation-value"}
	otelcol := v1alpha1.AmazonCloudWatchAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-instance",
		},
		Spec: v1alpha1.AmazonCloudWatchAgentSpec{
			PodAnnotations: testPodAnnotationValues,
		},
	}
	cfg := config.New()

	params := manifests.Params{
		Config:  cfg,
		OtelCol: otelcol,
		Log:     logger,
	}

	// test
	ds := DaemonSet(params)

	// Add sha256 podAnnotation
	testPodAnnotationValues["amazon-cloudwatch-agent-operator-config/sha256"] = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	expectedAnnotations := map[string]string{
		"annotation-key": "annotation-value",
		"amazon-cloudwatch-agent-operator-config/sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}

	// verify
	assert.Equal(t, "my-instance", ds.Name)
	assert.Len(t, ds.Spec.Template.Annotations, 5)
	assert.Equal(t, expectedAnnotations, ds.Spec.Template.Annotations)
}

func TestDaemonstPodSecurityContext(t *testing.T) {
	runAsNonRoot := true
	runAsUser := int64(1337)
	runasGroup := int64(1338)

	otelcol := v1alpha1.AmazonCloudWatchAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-instance",
		},
		Spec: v1alpha1.AmazonCloudWatchAgentSpec{
			PodSecurityContext: &v1.PodSecurityContext{
				RunAsNonRoot: &runAsNonRoot,
				RunAsUser:    &runAsUser,
				RunAsGroup:   &runasGroup,
			},
		},
	}

	cfg := config.New()

	params := manifests.Params{
		Config:  cfg,
		OtelCol: otelcol,
		Log:     logger,
	}

	d := DaemonSet(params)

	assert.Equal(t, &runAsNonRoot, d.Spec.Template.Spec.SecurityContext.RunAsNonRoot)
	assert.Equal(t, &runAsUser, d.Spec.Template.Spec.SecurityContext.RunAsUser)
	assert.Equal(t, &runasGroup, d.Spec.Template.Spec.SecurityContext.RunAsGroup)
}

func TestDaemonsetFilterLabels(t *testing.T) {
	excludedLabels := map[string]string{
		"foo":         "1",
		"app.foo.bar": "1",
	}

	otelcol := v1alpha1.AmazonCloudWatchAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "my-instance",
			Labels: excludedLabels,
		},
		Spec: v1alpha1.AmazonCloudWatchAgentSpec{},
	}

	cfg := config.New(config.WithLabelFilters([]string{"foo*", "app.*.bar"}))

	params := manifests.Params{
		Config:  cfg,
		OtelCol: otelcol,
		Log:     logger,
	}

	d := DaemonSet(params)

	assert.Len(t, d.ObjectMeta.Labels, 6)
	for k := range excludedLabels {
		assert.NotContains(t, d.ObjectMeta.Labels, k)
	}
}

func TestDaemonSetNodeSelector(t *testing.T) {
	// Test default
	otelcol1 := v1alpha1.AmazonCloudWatchAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-instance",
		},
	}

	cfg := config.New()

	params1 := manifests.Params{
		Config:  cfg,
		OtelCol: otelcol1,
		Log:     logger,
	}

	d1 := DaemonSet(params1)

	assert.Empty(t, d1.Spec.Template.Spec.NodeSelector)

	// Test nodeSelector
	otelcol2 := v1alpha1.AmazonCloudWatchAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-instance-nodeselector",
		},
		Spec: v1alpha1.AmazonCloudWatchAgentSpec{
			HostNetwork: true,
			NodeSelector: map[string]string{
				"node-key": "node-value",
			},
		},
	}

	cfg = config.New()

	params2 := manifests.Params{
		Config:  cfg,
		OtelCol: otelcol2,
		Log:     logger,
	}

	d2 := DaemonSet(params2)
	assert.Equal(t, d2.Spec.Template.Spec.NodeSelector, map[string]string{"node-key": "node-value"})
}

func TestDaemonSetPriorityClassName(t *testing.T) {
	otelcol1 := v1alpha1.AmazonCloudWatchAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-instance",
		},
	}

	cfg := config.New()

	params1 := manifests.Params{
		Config:  cfg,
		OtelCol: otelcol1,
		Log:     logger,
	}

	d1 := DaemonSet(params1)
	assert.Empty(t, d1.Spec.Template.Spec.PriorityClassName)

	priorityClassName := "test-class"

	otelcol2 := v1alpha1.AmazonCloudWatchAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-instance-priortyClassName",
		},
		Spec: v1alpha1.AmazonCloudWatchAgentSpec{
			PriorityClassName: priorityClassName,
		},
	}

	cfg = config.New()

	params2 := manifests.Params{
		Config:  cfg,
		OtelCol: otelcol2,
		Log:     logger,
	}

	d2 := DaemonSet(params2)
	assert.Equal(t, priorityClassName, d2.Spec.Template.Spec.PriorityClassName)
}

func TestDaemonSetAffinity(t *testing.T) {
	otelcol1 := v1alpha1.AmazonCloudWatchAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-instance",
		},
	}

	cfg := config.New()

	params1 := manifests.Params{
		Config:  cfg,
		OtelCol: otelcol1,
		Log:     logger,
	}

	d1 := DaemonSet(params1)
	assert.Nil(t, d1.Spec.Template.Spec.Affinity)

	otelcol2 := v1alpha1.AmazonCloudWatchAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name: "my-instance-priortyClassName",
		},
		Spec: v1alpha1.AmazonCloudWatchAgentSpec{
			Affinity: testAffinityValue,
		},
	}

	cfg = config.New()

	params2 := manifests.Params{
		Config:  cfg,
		OtelCol: otelcol2,
		Log:     logger,
	}

	d2 := DaemonSet(params2)
	assert.NotNil(t, d2.Spec.Template.Spec.Affinity)
	assert.Equal(t, *testAffinityValue, *d2.Spec.Template.Spec.Affinity)
}

func TestDaemonSetInitContainer(t *testing.T) {
	// prepare
	otelcol := v1alpha1.AmazonCloudWatchAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-instance",
			Namespace: "my-namespace",
		},
		Spec: v1alpha1.AmazonCloudWatchAgentSpec{
			InitContainers: []v1.Container{
				{
					Name: "test",
				},
			},
		},
	}
	cfg := config.New()

	params := manifests.Params{
		Config:  cfg,
		OtelCol: otelcol,
		Log:     logger,
	}

	// test
	d := DaemonSet(params)
	assert.Equal(t, "my-instance", d.Name)
	assert.Equal(t, "my-instance", d.Labels["app.kubernetes.io/name"])
	assert.Len(t, d.Spec.Template.Spec.InitContainers, 1)
}

func TestDaemonSetAdditionalContainer(t *testing.T) {
	// prepare
	otelcol := v1alpha1.AmazonCloudWatchAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-instance",
			Namespace: "my-namespace",
		},
		Spec: v1alpha1.AmazonCloudWatchAgentSpec{
			AdditionalContainers: []v1.Container{
				{
					Name: "test",
				},
			},
		},
	}
	cfg := config.New()

	params := manifests.Params{
		Config:  cfg,
		OtelCol: otelcol,
		Log:     logger,
	}

	// test
	d := DaemonSet(params)
	assert.Equal(t, "my-instance", d.Name)
	assert.Equal(t, "my-instance", d.Labels["app.kubernetes.io/name"])
	assert.Len(t, d.Spec.Template.Spec.Containers, 2)
	assert.Equal(t, v1.Container{Name: "test"}, d.Spec.Template.Spec.Containers[0])
}

func TestDaemonSetDefaultUpdateStrategy(t *testing.T) {
	// prepare
	otelcol := v1alpha1.AmazonCloudWatchAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-instance",
			Namespace: "my-namespace",
		},
		Spec: v1alpha1.AmazonCloudWatchAgentSpec{
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: "RollingUpdate",
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxSurge:       &intstr.IntOrString{Type: intstr.Int, IntVal: int32(1)},
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: int32(1)},
				},
			},
		},
	}
	cfg := config.New()

	params := manifests.Params{
		Config:  cfg,
		OtelCol: otelcol,
		Log:     logger,
	}

	// test
	d := DaemonSet(params)
	assert.Equal(t, "my-instance", d.Name)
	assert.Equal(t, "my-instance", d.Labels["app.kubernetes.io/name"])
	assert.Equal(t, appsv1.DaemonSetUpdateStrategyType("RollingUpdate"), d.Spec.UpdateStrategy.Type)
	assert.Equal(t, &intstr.IntOrString{Type: intstr.Int, IntVal: int32(1)}, d.Spec.UpdateStrategy.RollingUpdate.MaxSurge)
	assert.Equal(t, &intstr.IntOrString{Type: intstr.Int, IntVal: int32(1)}, d.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable)
}

func TestDaemonSetOnDeleteUpdateStrategy(t *testing.T) {
	// prepare
	otelcol := v1alpha1.AmazonCloudWatchAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-instance",
			Namespace: "my-namespace",
		},
		Spec: v1alpha1.AmazonCloudWatchAgentSpec{
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: "OnDelete",
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxSurge:       &intstr.IntOrString{Type: intstr.Int, IntVal: int32(1)},
					MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: int32(1)},
				},
			},
		},
	}
	cfg := config.New()

	params := manifests.Params{
		Config:  cfg,
		OtelCol: otelcol,
		Log:     logger,
	}

	// test
	d := DaemonSet(params)
	assert.Equal(t, "my-instance", d.Name)
	assert.Equal(t, "my-instance", d.Labels["app.kubernetes.io/name"])
	assert.Equal(t, appsv1.DaemonSetUpdateStrategyType("OnDelete"), d.Spec.UpdateStrategy.Type)
	assert.Equal(t, &intstr.IntOrString{Type: intstr.Int, IntVal: int32(1)}, d.Spec.UpdateStrategy.RollingUpdate.MaxSurge)
	assert.Equal(t, &intstr.IntOrString{Type: intstr.Int, IntVal: int32(1)}, d.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable)
}
