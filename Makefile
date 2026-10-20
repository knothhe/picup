GO ?= go
PYTHON ?= python3
export GOCACHE ?= /tmp/picup-go-cache
RELEASE_COMMAND ?= $(PYTHON) scripts/release.py
VERSION_BUMP ?= patch
RELEASE_FLAGS ?=
INSTALL_FLAGS ?=

.PHONY: build test check run package release release-initial install icons
build:
	mkdir -p build/bin
	$(GO) build -trimpath -o build/bin/picup ./cmd/picup
	$(GO) build -trimpath -o build/bin/picup-gui ./cmd/picup-gui

test:
	$(GO) test -race ./...

check:
	$(GO) vet ./...
	$(GO) test ./...
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) -m unittest discover -s scripts -p 'test_*.py'

run: build
	./build/bin/picup-gui

package: build
	$(GO) tool mygo build

release:
	$(RELEASE_COMMAND) $(VERSION_BUMP) $(RELEASE_FLAGS)

release-initial:
	$(RELEASE_COMMAND) initial $(RELEASE_FLAGS)

install:
	GO="$(GO)" $(GO) run ./cmd/install-local $(INSTALL_FLAGS)

icons:
	$(PYTHON) scripts/generate_icons.py
