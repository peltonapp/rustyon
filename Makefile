# SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
# SPDX-License-Identifier: BSD-3-Clause

GO       ?= go
FUZZTIME ?= 30s
PKGS     := ./...
# The CLI is a separate module (cmd/rustyon) so the library has no dependencies.
CLI      := cmd/rustyon

.PHONY: all test cover lint fmt vet golangci fuzz bench license build playground check clean

all: check

test:
	$(GO) test -race -count=1 $(PKGS)
	cd $(CLI) && $(GO) test -race -count=1 $(PKGS)

cover:
	$(GO) test -coverprofile=coverage.txt -covermode=atomic $(PKGS)
	$(GO) tool cover -html=coverage.txt -o coverage.html
	$(GO) tool cover -func=coverage.txt | tail -1
	cd $(CLI) && $(GO) test -coverprofile=coverage.txt -covermode=atomic $(PKGS)

fmt:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "Files need gofmt:"; echo "$$unformatted"; exit 1; fi

vet:
	$(GO) vet $(PKGS)
	cd $(CLI) && $(GO) vet $(PKGS)

golangci:
	golangci-lint run $(PKGS)
	cd $(CLI) && golangci-lint run $(PKGS)

lint: fmt vet golangci

fuzz:
	@for target in FuzzParse FuzzFormat FuzzUnmarshal; do \
		echo "== $$target"; \
		$(GO) test -run '^$$' -fuzz "^$$target$$" -fuzztime $(FUZZTIME) . || exit 1; \
	done

bench:
	$(GO) test -run '^$$' -bench . -benchmem .

# Every Go, YAML and Make file must carry an SPDX license header.
license:
	@missing="$$(git ls-files '*.go' '*.yml' '*.yaml' 'Makefile' | while read -r f; do \
		head -n 3 "$$f" | grep -q 'SPDX-License-Identifier: BSD-3-Clause' || echo "$$f"; \
	done)"; \
	if [ -n "$$missing" ]; then echo "Missing SPDX header:"; echo "$$missing"; exit 1; fi

build:
	cd $(CLI) && $(GO) build -trimpath -o ../../bin/rustyon .

# Build the WebAssembly playground into dist/playground. wasm_exec.js is
# copied from the local Go toolchain so it always matches the compiler.
playground:
	mkdir -p dist/playground
	GOOS=js GOARCH=wasm $(GO) build -trimpath -ldflags="-s -w" -o dist/playground/main.wasm ./playground/wasm
	cp "$$($(GO) env GOROOT)/lib/wasm/wasm_exec.js" dist/playground/wasm_exec.js
	cp playground/web/index.html dist/playground/index.html

check: lint test license

clean:
	rm -rf bin dist coverage.txt coverage.html
