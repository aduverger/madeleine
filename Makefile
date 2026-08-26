.PHONY: fmt-check test vet build check

fmt-check:
	@files="$$(gofmt -l .)"; test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }

test:
	go test ./...

vet:
	go vet ./...

build:
	go build ./...

check: fmt-check test vet build
