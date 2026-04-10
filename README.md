# jamle

**jamle** (**J**SON **A**nd Y**AML** with **E**nv)
provides a unified, powerful way to unmarshal YAML and JSON data
with Bash-style environment variable expansion in Go.

It includes a JSON-tag-aware YAML codec,
so one set of struct tags works for both formats.

## Key Benefits

* **Dynamic Configuration:**
  Inject environment variables directly into your YAML/JSON configs.
* **One Tag to Rule Them All:**
  You **don't need** `yaml` tags.
  * `jamle` YAML decoding is JSON-tag-aware, so it respects
    standard **`json` struct tags**.
  * One struct works for JSON and YAML inputs.

## Why use jamle

Modern applications (especially in Kubernetes or Docker)
often require dynamic configuration.  
`jamle` solves common problems when reading config files:

* **Inject Secrets:**
  Seamless usage of environment variables inside `config.yaml` or `config.json`
* **Set Defaults:**
  Define fallback values directly in the file
  (e.g., `${HOST:-localhost}` for local development)
* **Validation:**
  Force errors if required environment variables are missing using
  `${VAR:?error}`
* **Recursion:**
  Supports nested variables like `${HOST:=${DEFAULT_HOST}}`
* **Unified Parsing:**
  Forget about maintaining separate parsing logic for JSON and YAML

## Installation

### As a CLI Tool

You can download pre-compiled binaries from the
[Releases](https://github.com/woozymasta/jamle/releases) page,
or install directly via Go:

```bash
go install github.com/woozymasta/jamle/cmd/jamle@latest
```

### As a Library

To use `jamle` in your Go project:

```bash
go get github.com/woozymasta/jamle
```

## Supported Syntax

`jamle` supports Bash-style variable expansion,
including recursion and side effects:

Syntax            | Description
----------------- | -----------
`${VAR}`          | Value of `VAR`, or empty string if unset.
`${VAR:-default}` | Value of `VAR`, or "default" if `VAR` is unset or empty.
`${VAR:=default}` | Value of `VAR`, or "default" if unset/empty. **Also sets `VAR` in the current env.**
`${VAR:?error}`   | Value of `VAR`, or returns an error with "error" message if unset.
`$${VAR}`         | Escaping. Evaluates to the literal string `${VAR}` without expansion.

Note for JSON input:
placeholders with `:` operators should be used inside JSON strings.
Unquoted placeholders can break strict JSON syntax.

## Usage Example

Imagine you have a configuration file that needs to adapt between
Local, Staging, and Production environments.

### Create your config file (`config.yaml`)

```yaml
server:
  # Use env var or default to localhost
  host: "${SERVER_HOST:-localhost}"
  # Error if SERVER_PORT is not set
  port: ${SERVER_PORT:?port is required}

database:
  # Nested recursion: Use DB_URL, if missing use FULL_DSN
  dsn: "${DB_URL:-${FULL_DSN}}"
  # Sets env var 'DB_TIMEOUT' if it was missing
  timeout: "${DB_TIMEOUT:=30s}" 
```

### Parse it with CLI Tool

`jamle` comes with a handy command-line utility.
It reads YAML/JSON files, expands environment variables,
and outputs the result as formatted JSON.

This is perfect for:

* **Debugging:** Check how your config looks with current env vars
* **CI/CD Pipelines:** Pipe the output to tools like `jq` to extract values
* **Conversion:** Instantly convert YAML to JSON

Examples:

```bash
# Show help
jamle --help
# Read from file
jamle config.yaml
# Read from stdin and pipe to stdout
cat config.yaml | jamle | jq '.server.port'
# Write to file (auto by extension => YAML)
jamle config.yaml output.yaml
# Force output format explicitly
jamle config.yaml output.yaml --to yaml
# Disable required-variable errors (${VAR:?msg} behaves like ${VAR})
jamle config.yaml --disable-required-errors
# Set env var and read from file
export SERVER_PORT=9000
jamle config.yaml
```

### Parse it with Go

```go
package main

import (
    "fmt"
    "log"
    "os"

    "github.com/woozymasta/jamle"
)

type Config struct {
    Server struct {
        Host string `json:"host"` // Works for YAML via jamle JSON-tag-aware codec
        Port int    `json:"port"`
    } `json:"server"`
    Database struct {
        DSN     string `json:"dsn"`
        Timeout string `json:"timeout"`
    } `json:"database"`
}

func main() {
    // Simulate env vars
    os.Setenv("SERVER_PORT", "8080")

    data, _ := os.ReadFile("config.yaml")

    var cfg Config
    
    // Unmarshal with environment variable substitution
    if err := jamle.Unmarshal(data, &cfg); err != nil {
        log.Fatalf("Failed to parse config: %v", err)
    }

    fmt.Printf("Server: %s:%d\n", cfg.Server.Host, cfg.Server.Port)
    // Output: Server: localhost:8080
}
```

### Custom Resolver Example

Use `UnmarshalWithOptions` when variables come from a custom source
(for example, in-memory map, file-backed store, or secret manager adapter):

```go
package main

import "github.com/woozymasta/jamle"

type mapResolver map[string]string

func (r mapResolver) Lookup(name string) (string, bool) {
    v, ok := r[name]
    return v, ok
}

type Config struct {
    Host string `json:"host"`
    Port int    `json:"port"`
}

var cfg Config
_ = jamle.UnmarshalWithOptions([]byte(`
host: ${HOST:-localhost}
port: ${PORT:-8080}
`), &cfg, jamle.UnmarshalOptions{
    Resolver: mapResolver{
        "HOST": "svc.local",
        "PORT": "9000",
    },
})
```

### Hooks scripts: keep `${...}` literal

If a YAML field contains shell script with `${...}`,
you usually want to skip jamle expansion for that field.

Use struct tag `jamle:"noexpand"`:

```go
type Hook struct {
    Script string `json:"script" jamle:"noexpand"`
    Args   string `json:"args"`
}
```

Use path-based ignore rules for dynamic or external models:

```go
_ = jamle.UnmarshalWithOptions(data, &cfg, jamle.UnmarshalOptions{
    IgnoreExpandPaths: []string{
        "spec.hooks.*.*.script",
    },
})
```

If only one expression must stay literal in an expandable field,
use escaping:

```yaml
query: "${QUERY:-rate(http_requests[$${INTERVAL}])}"
```

## Features

* **JSON & YAML Support:**
  Works interchangeably on both formats.
* **Recursive Resolution:**
  Handles deeply nested variables (`${A:-${B}}`).
* **Multiple Documents:**
  `UnmarshalAll` decodes all documents from YAML streams (`---`).
* **Custom Variable Sources:**
  `UnmarshalWithOptions` supports non-env resolvers.
* **Type Safety:**
  Integers and floats in YAML are preserved correctly in the destination struct.
* **Loop Protection:**
  Built-in safeguards against infinite recursion loops.

## Additional `yaml` subpackage

`jamle` uses this subpackage internally
to implement the JSON-tag-aware YAML codec. You can also use
[`github.com/woozymasta/jamle/yaml`](https://pkg.go.dev/github.com/woozymasta/jamle/yaml)
directly as a drop-in style alternative to `github.com/invopop/yaml`.

Compared to `github.com/invopop/yaml` style usage, this package:

* uses `go.yaml.in/yaml/v3`;
* keeps JSON-tag-aware YAML decoding;
* avoids extra YAML <-> JSON byte conversion in the main unmarshal path.

```go
import "github.com/woozymasta/jamle/yaml"

type Config struct {
    Port int `json:"port"`
}

var cfg Config
_ = yaml.Unmarshal([]byte("port: 8080\n"), &cfg)
```

Simple helpers:

Read config from file.
Format resolution order for `ReadFile`:
`ReadOptions.Format` -> file extension -> content probe.

```go
var cfg Config
_ = yaml.ReadFile("config.auto", &cfg, yaml.ReadOptions{
    Format: yaml.FormatAuto,
})
```

Write file with explicit format and indentation via `WriteOptions`.

```go
_ = yaml.WriteFile("config.json", cfg, yaml.WriteOptions{
    Format: yaml.FormatJSON,
    Indent: 2,
})
```

Marshal to bytes without filesystem I/O.

```go
out, _ := yaml.MarshalWith(cfg, yaml.WriteOptions{
    Format: yaml.FormatYAML,
    Indent: 2,
})
_ = out
```

Caveats:

* `!!binary` is not preserved losslessly through `YAMLToJSON` conversion.
  Prefer plain base64 strings without the `!!binary` tag.
* `YAMLToJSON` may fail for YAML maps with non-JSON-compatible keys
  (for example, complex/map keys).
* `Unmarshal` decodes only the first document from multi-document YAML streams.
