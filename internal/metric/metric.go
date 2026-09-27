// Package metric defines the common contract between collectors and MQTT.
package metric

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

// DeviceRef groups related entities without changing their MQTT identities.
// An empty Kind keeps the entity on the server device.
type DeviceRef struct {
	Kind string
	Name string
}

type Sample struct {
	Device      DeviceRef
	Key         string
	Name        string
	Component   string
	Unit        string
	DeviceClass string
	StateClass  string
	Value       any
	Attributes  map[string]any
}

type Collector interface {
	Collect(context.Context) ([]Sample, error)
}

var unsafe = regexp.MustCompile(`[^a-z0-9_]+`)

// Key keeps arbitrary paths and service names distinct after slugification.
func Key(s string) string {
	sum := sha256.Sum256([]byte(s))
	slug := strings.Trim(unsafe.ReplaceAllString(strings.ToLower(s), "_"), "_")
	if len(slug) > 48 {
		slug = slug[:48]
	}
	if slug == "" {
		slug = "item"
	}
	return fmt.Sprintf("%s_%x", slug, sum[:4])
}

func Sensor(key, name, unit string, value any) Sample {
	return Sample{Key: key, Name: name, Component: "sensor", Unit: unit, Value: value}
}
func Binary(key, name string, online bool, attrs map[string]any) Sample {
	value := "OFF"
	if online {
		value = "ON"
	}
	return Sample{Key: key, Name: name, Component: "binary_sensor", DeviceClass: "connectivity", Value: value, Attributes: attrs}
}
