NPM_PACKAGE := @aduverger/madeleine-pi
PI_PACKAGE_DIR := harnesses/pi
NPM_PACKAGE_FILES := $(PI_PACKAGE_DIR)/package.json $(PI_PACKAGE_DIR)/package-lock.json
NPM_REGISTRY ?= https://registry.npmjs.org/
NPM_TAG ?= latest

.PHONY: fmt-check test vet build check pack-check release

GO_TEST_PACKAGES := ./cmd/... ./internal/... ./test/e2e
GO_BUILD_PACKAGES := ./cmd/... ./internal/...

fmt-check:
	@files="$$(gofmt -l cmd internal test)"; test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }

test:
	go test $(GO_TEST_PACKAGES)

vet:
	go vet $(GO_TEST_PACKAGES)

build:
	go build $(GO_BUILD_PACKAGES)

check: fmt-check test vet build

pack-check:
	cd $(PI_PACKAGE_DIR) && npm run pack:check

release:
	@test -n "$(VERSION)" || { echo "Usage: make release VERSION=x.y.z [NPM_TAG=latest]"; exit 1; }
	@node -e 'if (!/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$$/.test(process.argv[1])) { console.error("VERSION must be an explicit semantic version"); process.exit(1); }' "$(VERSION)"
	@release_version="$(VERSION)"; release_version="$${release_version%%+*}"; \
	case "$$release_version" in *-*) test "$(NPM_TAG)" != "latest" || { echo "Prereleases require NPM_TAG other than latest"; exit 1; };; esac
	@test "$$(git branch --show-current)" = "main" || { echo "Releases must run from main"; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "Working tree must be clean"; exit 1; }
	git fetch --prune --tags origin main
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/main)" || { echo "main must match origin/main"; exit 1; }
	@current_version="$$(node -p "require('./$(PI_PACKAGE_DIR)/package.json').version")"; \
	published_version="$$(npm view "$(NPM_PACKAGE)@$(VERSION)" version --registry="$(NPM_REGISTRY)" 2>/dev/null || true)"; \
	if test -n "$$published_version" && test "$$current_version" != "$(VERSION)"; then \
		echo "$(NPM_PACKAGE)@$(VERSION) exists but package.json is $$current_version"; exit 1; \
	fi; \
	if test -z "$$published_version"; then \
		npm whoami --registry="$(NPM_REGISTRY)" >/dev/null; \
	fi; \
	if git rev-parse -q --verify "refs/tags/v$(VERSION)" >/dev/null && test "$$current_version" != "$(VERSION)"; then \
		echo "Git tag v$(VERSION) exists but package.json is $$current_version"; exit 1; \
	fi; \
	if test "$$current_version" != "$(VERSION)"; then \
		cd $(PI_PACKAGE_DIR) && npm version "$(VERSION)" --no-git-tag-version; \
	fi
	$(MAKE) check
	$(MAKE) pack-check
	@if ! git diff --quiet -- $(NPM_PACKAGE_FILES); then \
		git add $(NPM_PACKAGE_FILES) && \
		git commit -m "Release $(VERSION)" && \
		git push origin main; \
	fi
	@if git rev-parse -q --verify "refs/tags/v$(VERSION)" >/dev/null; then \
		test "$$(git rev-parse "refs/tags/v$(VERSION)^{commit}")" = "$$(git rev-parse HEAD)" || \
			{ echo "Git tag v$(VERSION) points to another commit"; exit 1; }; \
	fi
	@published_version="$$(npm view "$(NPM_PACKAGE)@$(VERSION)" version --registry="$(NPM_REGISTRY)" 2>/dev/null || true)"; \
	if test "$$published_version" = "$(VERSION)"; then \
		published_head="$$(npm view "$(NPM_PACKAGE)@$(VERSION)" gitHead --registry="$(NPM_REGISTRY)" --prefer-online 2>/dev/null)"; \
		test "$$published_head" = "$$(git rev-parse HEAD)" || \
			{ echo "$(NPM_PACKAGE)@$(VERSION) was published from another commit"; exit 1; }; \
		echo "$(NPM_PACKAGE)@$(VERSION) is already published; resuming release"; \
	else \
		cd $(PI_PACKAGE_DIR) && npm publish --access public --tag "$(NPM_TAG)" --registry="$(NPM_REGISTRY)"; \
	fi
	@attempt=0; \
	until test "$$(npm view "$(NPM_PACKAGE)@$(VERSION)" version --registry="$(NPM_REGISTRY)" --prefer-online 2>/dev/null)" = "$(VERSION)"; do \
		attempt=$$((attempt + 1)); \
		test $$attempt -lt 61 || { echo "Published version was not visible in the registry after 10 minutes"; exit 1; }; \
		sleep 10; \
	done
	@if ! git rev-parse -q --verify "refs/tags/v$(VERSION)" >/dev/null; then \
		git tag -a "v$(VERSION)" -m "v$(VERSION)"; \
	fi
	git push origin "v$(VERSION)"
	@echo "Released $(NPM_PACKAGE)@$(VERSION) with npm tag $(NPM_TAG)"
