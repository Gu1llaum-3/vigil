# Default OS/ARCH values
OS ?= $(shell go env GOOS)
ARCH ?= $(shell go env GOARCH)
# Skip building the web UI if true
SKIP_WEB ?= false

# Set executable extension based on target OS
EXE_EXT := $(if $(filter windows,$(OS)),.exe,)

.PHONY: tidy build-agent build-hub build-hub-dev build clean lint dev-server dev-agent dev-hub dev generate-locales build-web-ui
.DEFAULT_GOAL := build

clean:
	go clean
	rm -rf ./build

lint:
	golangci-lint run

test:
	go test -tags=testing ./...

tidy:
	go mod tidy

build-web-ui:
	npm install --prefix ./internal/site
	npm run --prefix ./internal/site build

build-agent: tidy
	GOOS=$(OS) GOARCH=$(ARCH) go build -o ./build/vigil-agent_$(OS)_$(ARCH)$(EXE_EXT) -ldflags "-w -s" ./internal/cmd/agent

build-hub: tidy $(if $(filter false,$(SKIP_WEB)),build-web-ui)
	GOOS=$(OS) GOARCH=$(ARCH) go build -o ./build/vigil_$(OS)_$(ARCH)$(EXE_EXT) -ldflags "-w -s" ./internal/cmd/hub

build-hub-dev: tidy
	mkdir -p ./internal/site/dist && touch ./internal/site/dist/index.html
	GOOS=$(OS) GOARCH=$(ARCH) go build -tags development -o ./build/vigil-dev_$(OS)_$(ARCH)$(EXE_EXT) -ldflags "-w -s" ./internal/cmd/hub

build: build-agent build-hub

generate-locales:
	@if [ ! -f ./internal/site/src/locales/en/en.ts ]; then \
		echo "Generating locales..."; \
		npm install --prefix ./internal/site && npm run --prefix ./internal/site sync; \
	fi

dev-server: generate-locales
	npm run --prefix ./internal/site dev

dev-hub: export ENV=dev
dev-hub:
	mkdir -p ./internal/site/dist && touch ./internal/site/dist/index.html
	@if command -v entr >/dev/null 2>&1; then \
		find ./internal -type f -name '*.go' | entr -r -s "go run -tags development ./internal/cmd/hub serve --http 0.0.0.0:8090"; \
	else \
		go run -tags development ./internal/cmd/hub serve --http 0.0.0.0:8090; \
	fi

dev-agent:
	@if command -v entr >/dev/null 2>&1; then \
		find ./internal/cmd/agent/*.go ./agent/*.go | entr -r go run github.com/Gu1llaum-3/vigil/internal/cmd/agent; \
	else \
		go run github.com/Gu1llaum-3/vigil/internal/cmd/agent; \
	fi

# KEY="..." make dev
dev:
	$(MAKE) -j dev-server dev-hub dev-agent
