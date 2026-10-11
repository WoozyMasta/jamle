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
* **Lazy recursion:**
  Supports nested variables like `${HOST:=${DEFAULT_HOST}}` without
  evaluating fallback branches that are not selected.
* **Unified Parsing:**
  Forget about maintaining separate parsing logic for JSON and YAML

## Installation

### As a Library

To use `jamle` in your Go project:

```bash
go get github.com/woozymasta/jamle
```

### Auxiliary CLI

The repository also includes a small CLI utility
for basic configuration inspection and format conversion.

Latest releases are available on the
[GitHub Releases](https://github.com/woozymasta/jamle/releases) page.
Install the latest version with:

```bash
go install github.com/woozymasta/jamle/cmd/jamle@latest
```

Basic usage:

```bash
jamle config.yaml
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

Nested expressions are evaluated lazily.
Resolver cycles and expansion-depth exhaustion return errors;
they do not produce partially unresolved values.
Use `$${...}` when the literal `${...}` syntax must survive expansion.

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

### Strict decoding and defaults

Set `Strict` when decoding into a typed struct to reject unknown fields.
Map keys and values decoded into open interfaces remain unrestricted.

```go
type Config struct {
    Host string `json:"host"`
}

var cfg Config
err := jamle.UnmarshalWithOptions(
    []byte("host: localhost\nextra: true\n"),
    &cfg,
    jamle.UnmarshalOptions{Strict: true},
)
// err reports the unknown field "extra".
```

`default` tags fill only fields absent from the source document:

```go
type Config struct {
    Host string `json:"host" default:"localhost"`
    Port int    `json:"port" default:"8080"`
}
```

An explicit `0`, `false`, empty string,
or `null` is source data and is not replaced by a default.
Use `DisableDefaults: true` to skip default tags for one operation.
Do not also call `creasty/defaults.Set` on the same value
unless applying two different defaulting policies is intentional.
Default tag values are literal;
expansion syntax inside a `default` tag is not supported.

Mapping keys stay literal by default.
Enable `ExpandKeys` only when document deliberately uses expressions as keys:

```go
var cfg map[string]string
_ = jamle.UnmarshalWithOptions(
    []byte("${KEY}: ${VALUE:-default}\n"),
    &cfg,
    jamle.UnmarshalOptions{
        Resolver:   mapResolver{"KEY": "name"},
        ExpandKeys: true,
    },
)
```

The zero-value options keep `${VAR:=default}` assignment enabled.
With the default resolver, a selected assignment updates
the current process environment. Use `Assignment: jamle.AssignmentIgnore`
to keep the fallback without the side effect.

`Unmarshal` and `UnmarshalWithOptions` reject input containing more than one
YAML document. Use `UnmarshalAll` or `UnmarshalAllWithOptions` for streams:

```go
var documents []Config
_ = jamle.UnmarshalAll(data, &documents)
```

Expansion and ignore-path policies follow the effective consumer path
of YAML aliases and merge keys.
This keeps a policy attached to the field where the merged value is consumed.

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

Paths use dot-separated segments; `*`, `[0]`, and `[*]` match one segment.
Escape `.`, `[`, `]`, `*`, or `\` with a backslash
when the character is part of a literal key, for example `metadata\.v1.\*`.

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
  `Unmarshal` rejects streams;
  `UnmarshalAll` decodes all documents from YAML streams (`---`).
* **Custom Variable Sources:**
  `UnmarshalWithOptions` supports non-env resolvers.
* **Type Safety:**
  Integers and floats in YAML are preserved correctly in the destination struct.
* **Loop Protection:**
  Resolver cycles and expansion-depth exhaustion return errors.

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
* `Unmarshal` rejects multi-document YAML streams
  with `jamle.ErrMultipleDocuments`; use `UnmarshalAll` for streams.
