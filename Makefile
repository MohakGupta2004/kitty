BINARY   := kitty
APPNAME  := Kitty
PKG      := github.com/MohakGupta2004/desktop-kitty

VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# Bundle metadata needs a plain x.y.z, which a commit hash is not.
APP_VERSION ?= $(patsubst v%,%,$(shell git describe --tags --abbrev=0 2>/dev/null))
ifeq ($(strip $(APP_VERSION)),)
APP_VERSION := 0.0.0
endif
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# migrated_fynedo tells Fyne every UI call already goes through fyne.Do, which
# it does: see animate() in main.go.
TAGS     := migrated_fynedo
LDFLAGS  := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

FYNE     := go run fyne.io/tools/cmd/fyne@latest

.PHONY: all build run test vet fmt check package clean tidy

all: check build

build: ## Build the binary into ./dist
	@mkdir -p dist
	go build -tags "$(TAGS)" -ldflags "$(LDFLAGS)" -o dist/$(BINARY) .

run: build ## Build and run with verbose logging
	./dist/$(BINARY) -verbose

test: ## Run the tests with the race detector
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

check: vet test ## Everything CI checks

package: build ## Build a double-clickable Kitty.app (macOS only)
	@rm -rf dist/$(APPNAME).app
	$(FYNE) package -os darwin -name $(APPNAME) -icon icon.png \
		-app-version $(APP_VERSION) -executable dist/$(BINARY)
	@mv $(APPNAME).app dist/$(APPNAME).app
	@echo "built dist/$(APPNAME).app"

tidy:
	go mod tidy

clean:
	rm -rf dist $(APPNAME).app
