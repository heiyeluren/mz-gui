GO ?= go
NPM ?= npm

.PHONY: test test-go test-ui build build-ui run package-darwin check generate-app

test: test-go test-ui

test-go:
	$(GO) test -p=2 ./...

test-ui:
	$(NPM) --prefix framework/ui-kit test

build: build-ui
	$(GO) build ./...

build-ui:
	$(NPM) --prefix framework/ui-kit run build

run:
	$(GO) run ./examples/framework-showcase

package-darwin:
	bash scripts/package-showcase-darwin.sh

check:
	$(GO) run ./tools/mzguiframeworkcheck
	$(GO) run ./tools/mzguiappgen --check --manifest ./examples/framework-showcase/mzgui.app.toml --catalog ./framework/capabilities/catalog.json --output ./examples/framework-showcase/generated/appmeta/manifest_gen.go --package appmeta
	$(GO) test ./tests/architecture

generate-app:
	$(GO) run ./tools/mzguiappgen --manifest ./examples/framework-showcase/mzgui.app.toml --catalog ./framework/capabilities/catalog.json --output ./examples/framework-showcase/generated/appmeta/manifest_gen.go --package appmeta
