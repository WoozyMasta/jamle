RELEASE_MATRIX := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

CGO_ENABLED ?= 0
GOFLAGS     ?= -buildvcs=false -trimpath
LDFLAGS     ?= -s -w
GOWORK      ?= off

NATIVE_GOOS      := $(shell go env GOOS)
NATIVE_GOARCH    := $(shell go env GOARCH)
NATIVE_EXTENSION := $(if $(filter $(NATIVE_GOOS),windows),.exe,)

BINARY     ?= jamle
PKG        ?= ./cmd/jamle
OUTPUT_DIR ?= build
GO         ?= go

# Optional race flag for native build: make build RACE=1
RACE ?= 0
ifeq ($(RACE),1)
	EXTRA_BUILD_FLAGS := -race
endif

.PHONY: all clean build release test cover lint vet

all: test build

clean:
	rm -rf $(OUTPUT_DIR)
	rm -f coverage.out

build: clean
	@mkdir -p $(OUTPUT_DIR)
	@echo ">> building native: $(BINARY)$(NATIVE_EXTENSION)"
	GOOS=$(NATIVE_GOOS) GOARCH=$(NATIVE_GOARCH) \
	GOWORK=$(GOWORK) CGO_ENABLED=$(CGO_ENABLED) \
	$(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" $(EXTRA_BUILD_FLAGS) \
	-o $(OUTPUT_DIR)/$(BINARY)$(NATIVE_EXTENSION) $(PKG)

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
		$(GO) build $(GOFLAGS) -ldflags="$(LDFLAGS)" \
		-o $$out $(PKG); \
	done

lint:
	@echo ">> running golangci-lint"
	golangci-lint run ./...

vet:
	@echo ">> running go vet"
	$(GO) vet ./...

test:
	@echo ">> running tests"
	CGO_ENABLED=$(CGO_TEST_ENABLED) $(GO) test -v -cover $(TEST_FLAGS) ./...

cover:
	@echo ">> running tests with coverage"
	CGO_ENABLED=$(CGO_TEST_ENABLED) $(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out
