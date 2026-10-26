GO ?= go
PYTHON ?= python3
export GO
export BUILD_TAGS ?= picup_dev
export PYTHONDONTWRITEBYTECODE = 1
export GOCACHE ?= /tmp/picup-go-cache
RELEASE_COMMAND ?= $(PYTHON) scripts/release.py
VERSION_BUMP ?= patch
RELEASE_FLAGS ?=
INSTALL_FLAGS ?=
VERSION ?= $(shell $(PYTHON) -c 'import json; print(json.load(open("mygo.json"))["version"])')

.PHONY: build test check run package package-app package-ci package-release release release-initial release-assets install icons notices
build:
	$(PYTHON) scripts/build_binaries.py

test:
	$(GO) test -race ./...

check:
	$(GO) vet ./...
	$(GO) test ./...
	PYTHONDONTWRITEBYTECODE=1 $(PYTHON) -m unittest discover -s scripts -p 'test_*.py'

run: build
	./build/bin/picup-gui

package:
	$(PYTHON) scripts/build_binaries.py --package

package-app:
	$(PYTHON) scripts/build_binaries.py --package --skip-dmg

package-release:
	GO="$(GO)" $(PYTHON) scripts/package_release.py --version "$(VERSION)"

package-ci:
	GO="$(GO)" $(PYTHON) scripts/package_release.py --version "$(VERSION)" --unsigned --output-dir build/ci

notices:
	$(GO) run ./internal/buildinfo/cmd/notices -output build/THIRD_PARTY_NOTICES.txt

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
