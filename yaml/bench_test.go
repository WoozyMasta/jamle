package yaml

import "testing"

type benchNode struct {
	ID      int               `json:"id"`
	Name    string            `json:"name"`
	Enabled bool              `json:"enabled"`
	Labels  map[string]string `json:"labels"`
	Values  []float64         `json:"values"`
}

type benchDoc struct {
	Version int         `json:"version"`
	Meta    benchNode   `json:"meta"`
	Items   []benchNode `json:"items"`
}

var benchYAMLDoc = []byte(`
version: 3
meta:
  id: 1
  name: root
  enabled: true
  labels:
    env: prod
    app: jamle
  values: [1.1, 2.2, 3.3]
items:
  - id: 10
    name: api
    enabled: true
    labels: {tier: backend, role: public}
    values: [0.1, 0.2, 0.3]
  - id: 11
    name: worker
    enabled: false
    labels: {tier: backend, role: queue}
    values: [9.9, 8.8, 7.7]
  - id: 12
    name: web
    enabled: true
    labels: {tier: frontend, role: ui}
    values: [4.4, 5.5, 6.6]
`)

var benchJSONDoc = []byte(`{"version":3,"meta":{"id":1,"name":"root","enabled":true,"labels":{"env":"prod","app":"jamle"},"values":[1.1,2.2,3.3]},"items":[{"id":10,"name":"api","enabled":true,"labels":{"tier":"backend","role":"public"},"values":[0.1,0.2,0.3]},{"id":11,"name":"worker","enabled":false,"labels":{"tier":"backend","role":"queue"},"values":[9.9,8.8,7.7]},{"id":12,"name":"web","enabled":true,"labels":{"tier":"frontend","role":"ui"},"values":[4.4,5.5,6.6]}]}`)

func BenchmarkUnmarshal(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var doc benchDoc
		if err := Unmarshal(benchYAMLDoc, &doc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshal(b *testing.B) {
	doc := benchDoc{
		Version: 3,
		Meta: benchNode{
			ID:      1,
			Name:    "root",
			Enabled: true,
			Labels: map[string]string{
				"env": "prod",
				"app": "jamle",
			},
			Values: []float64{1.1, 2.2, 3.3},
		},
		Items: []benchNode{
			{
				ID:      10,
				Name:    "api",
				Enabled: true,
				Labels: map[string]string{
					"tier": "backend",
					"role": "public",
				},
				Values: []float64{0.1, 0.2, 0.3},
			},
			{
				ID:      11,
				Name:    "worker",
				Enabled: false,
				Labels: map[string]string{
					"tier": "backend",
					"role": "queue",
				},
				Values: []float64{9.9, 8.8, 7.7},
			},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := Marshal(doc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkYAMLToJSON(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := YAMLToJSON(benchYAMLDoc); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkJSONToYAML(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := JSONToYAML(benchJSONDoc); err != nil {
			b.Fatal(err)
		}
	}
}
