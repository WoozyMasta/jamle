package jamle

import "testing"

type benchConfig struct {
	Server struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	} `json:"server"`
	Database struct {
		URL     string `json:"url"`
		PoolMin int    `json:"pool_min"`
		PoolMax int    `json:"pool_max"`
		Enabled bool   `json:"enabled"`
	} `json:"database"`
	Features struct {
		Tracing bool   `json:"tracing"`
		Region  string `json:"region"`
		Name    string `json:"name"`
	} `json:"features"`
}

var benchmarkYAMLWithEnv = []byte(`
server:
  host: "${APP_HOST:localhost}"
  port: ${APP_PORT:8080}
database:
  url: "${DB_URL:postgres://user:pass@localhost:5432/app}"
  pool_min: ${DB_POOL_MIN:2}
  pool_max: ${DB_POOL_MAX:10}
  enabled: ${DB_ENABLED:true}
features:
  tracing: ${TRACING:false}
  region: "${REGION:eu-west-1}"
  name: "${SERVICE_NAME:jamle}"
`)

var benchmarkYAMLWithoutEnv = []byte(`
server:
  host: "localhost"
  port: 8080
database:
  url: "postgres://user:pass@localhost:5432/app"
  pool_min: 2
  pool_max: 10
  enabled: true
features:
  tracing: false
  region: "eu-west-1"
  name: "jamle"
`)

func BenchmarkUnmarshal_WithEnv(b *testing.B) {
	b.Setenv("APP_HOST", "prod.local")
	b.Setenv("APP_PORT", "9090")
	b.Setenv("DB_URL", "postgres://prod:prod@db:5432/prod")
	b.Setenv("DB_POOL_MIN", "4")
	b.Setenv("DB_POOL_MAX", "32")
	b.Setenv("DB_ENABLED", "true")
	b.Setenv("TRACING", "true")
	b.Setenv("REGION", "us-east-1")
	b.Setenv("SERVICE_NAME", "svc-prod")

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var cfg benchConfig
		if err := Unmarshal(benchmarkYAMLWithEnv, &cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_WithoutEnv(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var cfg benchConfig
		if err := Unmarshal(benchmarkYAMLWithoutEnv, &cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkExpandEnvInScalar(b *testing.B) {
	b.Setenv("A", "value-a")
	b.Setenv("B", "value-b")
	input := "${A:-x}-${B:-y}-${C:-${A}}-literal-$${NOPE}"

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		if _, err := expandEnvInScalar(input, runtimeOptions{
			resolver:        envResolver{},
			maxPasses:       defaultMaxPasses,
			allowAssignment: true,
		}); err != nil {
			b.Fatal(err)
		}
	}
}
