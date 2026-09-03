PACKAGE := @aduverger/pi-madeleine
PI_PACKAGE_DIR := harnesses/pi
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
	@node -e 'if (!/^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?$$/.test(process.argv[1])) { console.error("VERSION must be an explicit semantic version"); process.exit(1); }' "$(VERSION)"
	@case "$(VERSION)" in *-*) test "$(NPM_TAG)" != "latest" || { echo "Prereleases require NPM_TAG other than latest"; exit 1; };; esac
	@test "$$(git branch --show-current)" = "main" || { echo "Releases must run from main"; exit 1; }
	@test -z "$$(git status --porcelain)" || { echo "Working tree must be clean"; exit 1; }
	git fetch --prune --tags origin main
	@test "$$(git rev-parse HEAD)" = "$$(git rev-parse origin/main)" || { echo "main must match origin/main"; exit 1; }
	@npm whoami --registry="$(NPM_REGISTRY)" >/dev/null
	@if git rev-parse -q --verify "refs/tags/v$(VERSION)" >/dev/null; then echo "Git tag v$(VERSION) already exists"; exit 1; fi
	@if npm view "$(PACKAGE)@$(VERSION)" version --registry="$(NPM_REGISTRY)" >/dev/null 2>&1; then echo "$(PACKAGE)@$(VERSION) already exists"; exit 1; fi
	@if test "$$(node -p "require('./$(PI_PACKAGE_DIR)/package.json').version")" != "$(VERSION)"; then cd $(PI_PACKAGE_DIR) && npm version "$(VERSION)" --no-git-tag-version; fi
	$(MAKE) check
	$(MAKE) pack-check
	@if ! git diff --quiet -- $(PI_PACKAGE_DIR)/package.json $(PI_PACKAGE_DIR)/package-lock.json; then git add $(PI_PACKAGE_DIR)/package.json $(PI_PACKAGE_DIR)/package-lock.json && git commit -m "Release $(VERSION)" && git push origin main; fi
	cd $(PI_PACKAGE_DIR) && npm publish --access public --tag "$(NPM_TAG)" --registry="$(NPM_REGISTRY)"
	@attempt=0; until test "$$(npm view "$(PACKAGE)@$(VERSION)" version --registry="$(NPM_REGISTRY)" --prefer-online 2>/dev/null)" = "$(VERSION)"; do attempt=$$((attempt + 1)); test $$attempt -lt 61 || { echo "Published version was not visible in the registry after 10 minutes"; exit 1; }; sleep 10; done
	git tag -a "v$(VERSION)" -m "v$(VERSION)"
	git push origin "v$(VERSION)"
	@echo "Released $(PACKAGE)@$(VERSION) with npm tag $(NPM_TAG)"
