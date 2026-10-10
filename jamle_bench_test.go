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
  host: "${APP_HOST:-localhost}"
  port: ${APP_PORT:-8080}
database:
  url: "${DB_URL:-postgres://user:pass@localhost:5432/app}"
  pool_min: ${DB_POOL_MIN:-2}
  pool_max: ${DB_POOL_MAX:-10}
  enabled: ${DB_ENABLED:-true}
features:
  tracing: ${TRACING:-false}
  region: "${REGION:-eu-west-1}"
  name: "${SERVICE_NAME:-jamle}"
`)

var benchmarkYAMLFallback = []byte(`
server:
  host: "${APP_HOST:-localhost}"
  port: ${APP_PORT:-8080}
features:
  region: "${REGION:-eu-west-1}"
  name: "${SERVICE_NAME:-jamle}"
`)

var benchmarkYAMLNestedLazy = []byte(`
server:
  host: "${APP_HOST:-${FALLBACK_HOST:-localhost}}"
`)

var benchmarkYAMLEscaped = []byte(`
server:
  host: "$${APP_HOST:-localhost}"
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

var benchmarkYAMLDefaults = []byte(`
server: {}
database: {}
`)

var benchmarkYAMLNoExpand = []byte(`
script: "${SCRIPT:-literal}"
expanded: "${EXPANDED:-expanded}"
items:
  - script: "${ITEM_SCRIPT:-literal}"
    expanded: "${ITEM_EXPANDED:-expanded}"
`)

var benchmarkYAMLAll = []byte(`
---
server:
  host: "${APP_HOST:-localhost}"
  port: ${APP_PORT:-8080}
---
server:
  host: "${APP_HOST:-localhost}"
  port: ${APP_PORT:-8080}
`)

// benchDefaultsConfig measures the source-aware defaults stage.
type benchDefaultsConfig struct {
	Server struct {
		Host string `json:"host" default:"localhost"`
		Port int    `json:"port" default:"8080"`
	} `json:"server"`
	Database struct {
		URL     string `json:"url" default:"postgres://localhost/app"`
		PoolMin int    `json:"pool_min" default:"2"`
		PoolMax int    `json:"pool_max" default:"10"`
	} `json:"database"`
}

// benchNoExpandItem measures schema-aware noexpand traversal in collections.
type benchNoExpandItem struct {
	Script   string `json:"script" jamle:"noexpand"`
	Expanded string `json:"expanded"`
}

// benchNoExpandConfig measures schema-aware noexpand traversal.
type benchNoExpandConfig struct {
	Script   string              `json:"script" jamle:"noexpand"`
	Expanded string              `json:"expanded"`
	Items    []benchNoExpandItem `json:"items"`
}

// benchNestedConfig measures nested lazy fallback expansion.
type benchNestedConfig struct {
	Server struct {
		Host string `json:"host"`
	} `json:"server"`
}

// benchEscapedConfig measures escaped expansion syntax.
type benchEscapedConfig struct {
	Server struct {
		Host string `json:"host"`
	} `json:"server"`
}

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

func BenchmarkUnmarshal_SchemaCacheWarm(b *testing.B) {
	var cfg benchConfig
	if err := Unmarshal(benchmarkYAMLWithoutEnv, &cfg); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var cfg benchConfig
		if err := Unmarshal(benchmarkYAMLWithoutEnv, &cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_Fallback(b *testing.B) {
	for _, name := range []string{
		"APP_HOST",
		"APP_PORT",
		"REGION",
		"SERVICE_NAME",
	} {
		b.Setenv(name, "")
	}

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var cfg benchConfig
		if err := Unmarshal(benchmarkYAMLFallback, &cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_NestedLazy(b *testing.B) {
	b.Setenv("APP_HOST", "configured.local")
	b.Setenv("FALLBACK_HOST", "")
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var cfg benchNestedConfig
		if err := Unmarshal(benchmarkYAMLNestedLazy, &cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_Escaped(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var cfg benchEscapedConfig
		if err := Unmarshal(benchmarkYAMLEscaped, &cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_Strict(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var cfg benchConfig
		if err := UnmarshalWithOptions(benchmarkYAMLWithoutEnv, &cfg, UnmarshalOptions{
			Strict: true,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_Defaults(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var cfg benchDefaultsConfig
		if err := Unmarshal(benchmarkYAMLDefaults, &cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_StrictDefaults(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var cfg benchDefaultsConfig
		if err := UnmarshalWithOptions(benchmarkYAMLDefaults, &cfg, UnmarshalOptions{
			Strict: true,
		}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshal_NoExpand(b *testing.B) {
	b.Setenv("SCRIPT", "configured")
	b.Setenv("EXPANDED", "configured")
	b.Setenv("ITEM_SCRIPT", "configured")
	b.Setenv("ITEM_EXPANDED", "configured")
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var cfg benchNoExpandConfig
		if err := Unmarshal(benchmarkYAMLNoExpand, &cfg); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnmarshalAll_WithEnv(b *testing.B) {
	b.Setenv("APP_HOST", "prod.local")
	b.Setenv("APP_PORT", "9090")
	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		var configs []benchConfig
		if err := UnmarshalAll(benchmarkYAMLAll, &configs); err != nil {
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
			maxDepth:        defaultMaxDepth,
			allowAssignment: true,
		}); err != nil {
			b.Fatal(err)
		}
	}
}
