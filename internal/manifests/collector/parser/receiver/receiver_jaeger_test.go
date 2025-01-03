// Copyright Amazon.com, Inc. or its affiliates. All Rights Reserved.
// SPDX-License-Identifier: Apache-2.0

package receiver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestJaegerSelfRegisters(t *testing.T) {
	// verify
	assert.True(t, IsRegistered("jaeger"))
}

func TestJaegerIsFoundByName(t *testing.T) {
	// test
	p, err := For(logger, "jaeger", map[interface{}]interface{}{})
	assert.NoError(t, err)

	// verify
	assert.Equal(t, "__jaeger", p.ParserName())
}

func TestJaegerMinimalConfiguration(t *testing.T) {
	// prepare
	builder := NewJaegerReceiverParser(logger, "jaeger", map[interface{}]interface{}{
		"protocols": map[interface{}]interface{}{
			"grpc": map[interface{}]interface{}{},
		},
	})

	// test
	ports, err := builder.Ports()

	// verify
	assert.NoError(t, err)
	assert.Len(t, ports, 1)
	assert.EqualValues(t, 14250, ports[0].Port)
	assert.EqualValues(t, corev1.ProtocolTCP, ports[0].Protocol)
}

func TestJaegerPortsOverridden(t *testing.T) {
	// prepare
	builder := NewJaegerReceiverParser(logger, "jaeger", map[interface{}]interface{}{
		"protocols": map[interface{}]interface{}{
			"grpc": map[interface{}]interface{}{
				"endpoint": "0.0.0.0:1234",
			},
		},
	})

	// test
	ports, err := builder.Ports()

	// verify
	assert.NoError(t, err)
	assert.Len(t, ports, 1)
	assert.EqualValues(t, 1234, ports[0].Port)
	assert.EqualValues(t, corev1.ProtocolTCP, ports[0].Protocol)
}

func TestJaegerExposeDefaultPorts(t *testing.T) {
	// prepare
	builder := NewJaegerReceiverParser(logger, "jaeger", map[interface{}]interface{}{
		"protocols": map[interface{}]interface{}{
			"grpc":           map[interface{}]interface{}{},
			"thrift_http":    map[interface{}]interface{}{},
			"thrift_compact": map[interface{}]interface{}{},
			"thrift_binary":  map[interface{}]interface{}{},
		},
	})

	expectedResults := map[string]struct {
		transportProtocol corev1.Protocol
		portNumber        int32
		seen              bool
	}{
		"jaeger-grpc": {portNumber: 14250, transportProtocol: corev1.ProtocolTCP},
		"port-14268":  {portNumber: 14268, transportProtocol: corev1.ProtocolTCP},
		"port-6831":   {portNumber: 6831, transportProtocol: corev1.ProtocolUDP},
		"port-6832":   {portNumber: 6832, transportProtocol: corev1.ProtocolUDP},
	}

	// test
	ports, err := builder.Ports()

	// verify
	assert.NoError(t, err)
	assert.Len(t, ports, 4)

	for _, port := range ports {
		r := expectedResults[port.Name]
		r.seen = true
		expectedResults[port.Name] = r
		assert.EqualValues(t, r.portNumber, port.Port)
		assert.EqualValues(t, r.transportProtocol, port.Protocol)
	}
	for k, v := range expectedResults {
		assert.True(t, v.seen, "the port %s wasn't included in the service ports", k)
	}
}
