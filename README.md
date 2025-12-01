# jamle

**jamle** (**J**SON **A**nd Y**AML** with **E**nv)
provides a unified, powerful way to unmarshal YAML and JSON data
with Bash-style environment variable expansion in Go.

It acts as a smart wrapper around the robust
[`github.com/invopop/yaml`](https://github.com/invopop/yaml).

## Key Benefits

* **Dynamic Configuration:**
  Inject environment variables directly into your YAML/JSON configs.
* **One Tag to Rule Them All:**
  You **don't need** `yaml` tags.

  * Since `jamle` converts YAML to JSON internally before parsing,
    it fully respects standard **`json` struct tags**.
  * It also supports custom `MarshalJSON` and `UnmarshalJSON`
    methods for YAML data automatically.

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
`${VAR:default}`  | Shorthand for the above.
`${VAR:=default}` | Value of `VAR`, or "default" if unset/empty. **Also sets `VAR` in the current env.**
`${VAR:?error}`   | Value of `VAR`, or returns an error with "error" message if unset.
`$${VAR}`         | Escaping. Evaluates to the literal string `${VAR}` without expansion.

## Usage Example

Imagine you have a configuration file that needs to adapt between
Local, Staging, and Production environments.

### Create your config file (`config.yaml`)

```yaml
server:
  # Use env var or default to localhost
  host: "${SERVER_HOST:localhost}"
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

Read from file:

```bash
# Read from file
jamle config.yaml
# Read from stdin (pipe)
cat config.yaml | jamle | jq '.server.port'
jamle config.yaml | yq '.server.port'
# Verify config
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
        Host string `json:"host"` // Works for YAML thanks to invopop/yaml
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

## Features

* **JSON & YAML Support:**
  Works interchangeably on both formats.
* **Recursive Resolution:**
  Handles deeply nested variables (`${A:${B}}`).
* **Type Safety:**
  Integers and floats in YAML are preserved correctly in the destination struct.
* **Loop Protection:**
  Built-in safeguards against infinite recursion loops.
