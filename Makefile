GO ?= go

.PHONY: check test build fmt install-hooks release-artifacts smoke-release-install release-track-version start stop status web-build

check:
	bash scripts/web/build-admin-ui.sh
	bash scripts/externalaccess/prepare-cloudflared-embed.sh
	bash scripts/check/no-local-paths.sh
	bash scripts/check/no-legacy-names.sh
	files="$$(find cmd internal testkit -name '*.go' | sort)"; output="$$(gofmt -l $$files)"; test -z "$$output" || (echo "$$output" >&2; exit 1)
	bash scripts/check/release-track-version.sh
	bash scripts/check/smoke-install-release.sh
	$(GO) test ./...

test:
	bash scripts/externalaccess/prepare-cloudflared-embed.sh
	$(GO) test ./...

build:
	bash scripts/web/build-admin-ui.sh
	bash scripts/externalaccess/prepare-cloudflared-embed.sh
	$(GO) build ./cmd/codex-remote
	$(GO) build ./cmd/relayd
	$(GO) build ./cmd/relay-wrapper
	$(GO) build ./cmd/relay-install

web-build:
	bash scripts/web/build-admin-ui.sh

fmt:
	gofmt -w $$(find cmd internal testkit -name '*.go' | sort)

install-hooks:
	bash scripts/dev/install-git-hooks.sh

release-artifacts:
	@test -n "$(VERSION)" || (echo "VERSION is required, e.g. make release-artifacts VERSION=v0.1.0" >&2; exit 1)
	bash scripts/release/build-artifacts.sh "$(VERSION)"

smoke-release-install:
	bash scripts/check/smoke-install-release.sh

release-track-version:
	bash scripts/check/release-track-version.sh

start:
	bash scripts/externalaccess/prepare-cloudflared-embed.sh
	mkdir -p bin
	$(GO) build -o ./bin/codex-remote ./cmd/codex-remote
	./bin/codex-remote install -bootstrap-only -start-daemon

stop:
	@echo "No repo-local stop helper is provided." >&2
	@echo "Stop the codex-remote daemon process directly, e.g.:" >&2
	@echo "  curl -X POST http://127.0.0.1:9501/v1/stop" >&2
	@echo "Or send SIGINT/SIGTERM to the daemon process manually." >&2
	@exit 1

status:
	@echo "Query the daemon status via the admin API:" >&2
	@echo "  curl http://127.0.0.1:9501/v1/status" >&2
	@echo "Or inspect the daemon process directly." >&2
	@exit 1
