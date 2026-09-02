.PHONY: fmt-check test vet build check

GO_PACKAGES := ./cmd/... ./internal/... ./test/e2e
GO_BUILD_PACKAGES := ./cmd/... ./internal/...

fmt-check:
	@files="$$(gofmt -l cmd internal test)"; test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }

test:
	go test $(GO_PACKAGES)

vet:
	go vet $(GO_PACKAGES)

build:
	go build $(GO_BUILD_PACKAGES)

check: fmt-check test vet build
