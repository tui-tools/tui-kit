# tui-kit — the shared foundation of the tui-tools family.

GO ?= go

.PHONY: all check check-exec test vet fmt fmt-check lint tidy branding clean

all: check

## test: run the unit tests.
test:
	$(GO) test ./...

## vet: run the standard static checks.
vet:
	$(GO) vet ./...

## fmt: rewrite the sources with gofmt.
fmt:
	gofmt -w .

## fmt-check: fail when something is not gofmt-clean.
fmt-check:
	@out=$$(gofmt -l .); \
	if [ -n "$$out" ]; then \
		echo "these files need gofmt:"; echo "$$out"; exit 1; \
	fi

## lint: fmt-check, vet and the exec boundary. golangci-lint when installed.
lint: fmt-check vet check-exec
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed, skipping"; \
	fi

## check-exec: assert that only the runner starts a process.
check-exec:
	bash tools/check-exec.sh .

## check: everything CI runs.
check: lint test

## tidy: prune and refresh go.mod / go.sum.
tidy:
	$(GO) mod tidy

## branding: regenerate the family logos and icons from their SVG sources.
branding:
	python3 tools/render-branding.py --out assets/branding

## clean: remove build output.
clean:
	rm -rf dist
