GO ?= go
PYTHON ?= python3
export GOCACHE ?= /tmp/picup-go-cache
RELEASE_COMMAND ?= $(PYTHON) scripts/release.py
VERSION_BUMP ?= patch
RELEASE_FLAGS ?=
INSTALL_FLAGS ?=
VERSION ?= $(shell $(PYTHON) -c 'import json; print(json.load(open("mygo.json"))["version"])')

.PHONY: build test check run package package-release release release-initial release-assets install icons
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

package-release:
	GO="$(GO)" $(PYTHON) scripts/package_release.py --version "$(VERSION)"

release:
	GO="$(GO)" $(RELEASE_COMMAND) $(VERSION_BUMP) $(RELEASE_FLAGS)

release-initial:
	GO="$(GO)" $(RELEASE_COMMAND) initial $(RELEASE_FLAGS)

release-assets:
	GO="$(GO)" $(RELEASE_COMMAND) "$(VERSION)" --assets-only $(RELEASE_FLAGS)

install:
	GO="$(GO)" $(GO) run ./cmd/install-local $(INSTALL_FLAGS)

icons:
	$(PYTHON) scripts/generate_icons.py
