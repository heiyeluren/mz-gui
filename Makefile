GO ?= go
NPM ?= npm

.PHONY: test test-go test-ui build build-ui run package-darwin check generate-app

test: test-go test-ui test-ui-e2e

test-go:
	$(GO) test -p=2 ./...

test-ui:
	$(NPM) --prefix framework/ui-kit test

test-ui-e2e:
	$(NPM) --prefix framework/ui-kit run test:e2e

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
	$(GO) run ./tools/mzguiapidiff --old framework/api/baselines/v0.0.1.json --new framework/api/public-v1.json --caller-usage framework/api/caller-usage/framework-showcase.json --caller-usage framework/api/caller-usage/framework-plugin-echo.json --callsite-caller framework-showcase --callsite-root examples/framework-showcase
	$(GO) test ./tests/architecture

generate-app:
	$(GO) run ./tools/mzguiappgen --manifest ./examples/framework-showcase/mzgui.app.toml --catalog ./framework/capabilities/catalog.json --output ./examples/framework-showcase/generated/appmeta/manifest_gen.go --package appmeta
