.PHONY: fmt-check test vet build check

fmt-check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; exit 1; }

test:
	go test ./...

vet:
	go vet ./...

build:
	go build ./...

check: fmt-check test vet build
