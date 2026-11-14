# SPDX-FileCopyrightText: 2026 Arne K. (TRC-Loop)
# SPDX-License-Identifier: BSD-3-Clause

GO       ?= go
FUZZTIME ?= 30s
PKGS     := ./...

.PHONY: all test cover lint fmt vet golangci fuzz bench license build check clean

all: check

test:
	$(GO) test -race -count=1 $(PKGS)

cover:
	$(GO) test -coverprofile=coverage.txt -covermode=atomic $(PKGS)
	$(GO) tool cover -html=coverage.txt -o coverage.html
	$(GO) tool cover -func=coverage.txt | tail -1

fmt:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then echo "Files need gofmt:"; echo "$$unformatted"; exit 1; fi

vet:
	$(GO) vet $(PKGS)

golangci:
	golangci-lint run $(PKGS)

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
	$(GO) build -trimpath -o bin/rustyon ./cmd/rustyon

check: lint test license

clean:
	rm -rf bin dist coverage.txt coverage.html
