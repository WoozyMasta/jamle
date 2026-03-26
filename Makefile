GO          ?= go
LINTER      ?= golangci-lint
ALIGNER     ?= betteralign
VULNCHECK   ?= govulncheck
BENCHSTAT   ?= benchstat
CYCLO       ?= cyclonedx-gomod

CGO_ENABLED ?= 0
GOFLAGS     ?= -buildvcs=auto -trimpath
LDFLAGS     ?= -s -w
GOWORK      ?= off
GOFTAGS     ?= forceposix

BENCH_COUNT ?= 6
BENCH_REF   ?= bench_baseline.txt
MODULE_PATH ?= $(shell GOWORK=off $(GO) list -m -f '{{.Path}}')

BINARY      ?= jamle
PKG         ?= ./cmd/jamle
OUTPUT_DIR  ?= build

RELEASE_MATRIX ?= linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

NATIVE_GOOS      := $(shell go env GOOS)
NATIVE_GOARCH    := $(shell go env GOARCH)
NATIVE_EXTENSION := $(if $(filter $(NATIVE_GOOS),windows),.exe,)

VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null || echo v0.0.0)
COMMIT  := $(shell git rev-parse HEAD 2>/dev/null || echo unknown)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
URL     ?= https://$(MODULE_PATH)

LDFLAGS_X := \
	-X 'main.Version=$(VERSION)' \
	-X 'main.Commit=$(COMMIT)' \
	-X 'main._buildTime=$(DATE)' \
	-X 'main.URL=$(URL)'

RACE ?= 0
ifeq ($(RACE),1)
	EXTRA_BUILD_FLAGS := -race
endif

.PHONY: test test-race test-short bench bench-fast bench-reset verify vet check ci \
	fmt fmt-check lint lint-fix align align-fix tidy tidy-check download deps-update \
	tools tools-ci tool-golangci-lint tool-betteralign tool-govulncheck tool-benchstat tool-cyclonedx \
	release-notes sbom sbom-app sbom-bin

check: verify vulncheck tidy fmt vet lint-fix align-fix test
ci: download tools-ci verify vulncheck tidy-check fmt-check vet lint align test

clean:
	rm -rf $(OUTPUT_DIR)

build: clean
	@mkdir -p $(OUTPUT_DIR)
	@echo ">> building native: $(BINARY)$(NATIVE_EXTENSION)"
	GOOS=$(NATIVE_GOOS) GOARCH=$(NATIVE_GOARCH) \
	GOWORK=$(GOWORK) CGO_ENABLED=$(CGO_ENABLED) \
	$(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS) $(LDFLAGS_X)" -tags "$(GOFTAGS)" $(EXTRA_BUILD_FLAGS) \
	-o $(OUTPUT_DIR)/$(BINARY)$(NATIVE_EXTENSION) $(PKG)
	@$(MAKE) _sbom_bin_one GOOS=$(NATIVE_GOOS) GOARCH=$(NATIVE_GOARCH) BIN=$(BINARY) OUTEXT="$(NATIVE_EXTENSION)"

release: clean
	@mkdir -p $(OUTPUT_DIR)
	@for target in $(RELEASE_MATRIX); do \
		goos=$${target%%/*}; \
		goarch=$${target##*/}; \
		ext=$$( [ $$goos = "windows" ] && echo ".exe" || echo "" ); \
		out="$(OUTPUT_DIR)/$(BINARY)-$${goos}-$${goarch}$$ext"; \
		echo ">> building $$out"; \
		GOOS=$$goos GOARCH=$$goarch \
		GOWORK=$(GOWORK) CGO_ENABLED=$(CGO_ENABLED) \
		$(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS) $(LDFLAGS_X)" -tags "$(GOFTAGS)" \
		-o $$out $(PKG); \
		$(MAKE) --no-print-directory _sbom_bin_one GOOS=$$goos GOARCH=$$goarch BIN=$(BINARY)-$${goos}-$${goarch} OUTEXT="$$ext"; \
	done
	@$(MAKE) sbom-app

fmt:
	gofmt -w .

fmt-check:
	@files=$$(gofmt -l .); \
	if [ -n "$$files" ]; then \
		echo "$$files" 1>&2; \
		echo "gofmt: files need formatting" 1>&2; \
		exit 1; \
	fi

vet:
	$(GO) vet ./...

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

test-short:
	$(GO) test -short ./...

bench:
	@tmp=$$(mktemp); \
	$(GO) test ./... -run=^$$ -bench 'Benchmark' -benchmem -count=$(BENCH_COUNT) | tee "$$tmp"; \
	if [ -f "$(BENCH_REF)" ]; then \
		$(BENCHSTAT) "$(BENCH_REF)" "$$tmp"; \
	else \
		cp "$$tmp" "$(BENCH_REF)" && echo "Baseline saved to $(BENCH_REF)"; \
	fi; \
	rm -f "$$tmp"

bench-fast:
	$(GO) test ./... -run=^$$ -bench 'Benchmark' -benchmem

bench-reset:
	rm -f "$(BENCH_REF)"

verify:
	$(GO) mod verify

tidy-check:
	@$(GO) mod tidy
	@git diff --stat --exit-code -- go.mod go.sum || ( \
		echo "go mod tidy: repository is not tidy"; \
		exit 1; \
	)

tidy:
	$(GO) mod tidy

download:
	$(GO) mod download

deps-update:
	$(GO) get -u ./...
	$(GO) mod tidy

lint:
	$(LINTER) run ./...

lint-fix:
	$(LINTER) run --fix ./...

align:
	$(ALIGNER) ./...

align-fix:
	-$(ALIGNER) -apply ./...
	$(ALIGNER) ./...

vulncheck:
	$(VULNCHECK) ./...

tools: tool-golangci-lint tool-betteralign tool-govulncheck tool-benchstat tool-cyclonedx
tools-ci: tool-golangci-lint tool-betteralign tool-govulncheck tool-cyclonedx

tool-golangci-lint:
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest

tool-betteralign:
	$(GO) install github.com/dkorunic/betteralign/cmd/betteralign@latest

tool-govulncheck:
	$(GO) install golang.org/x/vuln/cmd/govulncheck@latest

tool-benchstat:
	$(GO) install golang.org/x/perf/cmd/benchstat@latest

tool-cyclonedx:
	$(GO) install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@latest

release-notes:
	@awk '\
	/^<!--/,/^-->/ { next } \
	/^## \[[0-9]+\.[0-9]+\.[0-9]+\]/ { if (found) exit; found=1; next } \
	found { \
		if (/^## \[/) { exit } \
		if (/^$$/) { flush(); print; next } \
		if (/^\* / || /^- /) { flush(); buf=$$0; next } \
		if (/^###/ || /^\[/) { flush(); print; next } \
		sub(/^[ \t]+/, ""); sub(/[ \t]+$$/, ""); \
		if (buf != "") { buf = buf " " $$0 } else { buf = $$0 } \
		next \
	} \
	function flush() { if (buf != "") { print buf; buf = "" } } \
	END { flush() } \
	' CHANGELOG.md

sbom: sbom-app sbom-bin

sbom-app:
	@echo ">> SBOM (app)"
	$(CYCLO) app -json -packages -files -licenses \
		-output "$(OUTPUT_DIR)/$(BINARY).sbom.json" -main "$(PKG)"

sbom-bin:
	@echo ">> SBOM (bin native if exists)"
	@[ -f "$(OUTPUT_DIR)/$(BINARY)$(NATIVE_EXTENSION)" ] && \
		$(CYCLO) bin -json -output "$(OUTPUT_DIR)/$(BINARY)$(NATIVE_EXTENSION).sbom.json" \
			"$(OUTPUT_DIR)/$(BINARY)$(NATIVE_EXTENSION)" || true

_sbom_bin_one:
	@bin="$(OUTPUT_DIR)/$(BIN)$(OUTEXT)"; \
	if [ -f "$$bin" ]; then \
		echo ">> SBOM (bin) $$bin"; \
		$(CYCLO) bin -json -output "$$bin.sbom.json" "$$bin"; \
	fi
