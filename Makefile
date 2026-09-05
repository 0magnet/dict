# The demo page. `make demo` is the one that matters: it is what is
# committed at the repository root — which is what GitHub Pages serves,
# so that data/dictd is reachable from the page — and it takes about two
# seconds.
#
# The TinyGo build is a third the size and takes minutes rather than
# seconds, because its compile-time interpreter has to fold a 3.3 MB
# embedded corpus on every build. A demo that is expensive to rebuild is
# a demo that goes stale, so the fast one is the committed one and the
# small one stays a command away.

GOFILES := $(shell find . -name '*.go' -not -path './docs/*' -not -path './vendor/*')

.PHONY: demo demo-tinygo serve clean test test-wasm lint

demo: dict.wasm wasm_exec.js ## build the demo page (standard Go, ~2s)

dict.wasm: $(GOFILES)
	GOOS=js GOARCH=wasm go build -tags dictlite -ldflags="-s -w" -o $@ ./web/term/

wasm_exec.js:
	cp "$$(go env GOROOT)/lib/wasm/wasm_exec.js" $@

demo-tinygo: ## build the demo with TinyGo instead (a third the size, minutes not seconds)
	@command -v tinygo >/dev/null 2>&1 || { echo "tinygo not installed"; exit 1; }
	GOTOOLCHAIN=$$(sh scripts/tinygo-toolchain.sh) tinygo build -target wasm -tags dictlite \
		-no-debug -opt=z -o dict.wasm ./web/term/
	cp "$$(GOTOOLCHAIN=$$(sh scripts/tinygo-toolchain.sh) tinygo env TINYGOROOT)/targets/wasm_exec.js" wasm_exec.js

serve: demo ## serve the demo at http://127.0.0.1:8791
	go run ./serve -dir . -addr :8791

clean:
	rm -f dict.wasm

# The targets the shared CI gate calls. Written out here rather than taken from
# the template Makefile, which this repo does not use: the demo build above is
# the point of this one.

test: ## Run the host tests
	go test ./...

test-wasm: ## Compile-gate the js/wasm-tagged half
	@# A BUILD, not a test run: a host build cannot see //go:build js && wasm
	@# files at all, so without this a wasm-only break stays invisible — and
	@# most of this repo is behind that tag.
	@if ! grep -rlq '^//go:build js' --include='*.go' --exclude-dir=vendor . 2>/dev/null; then \
		echo 'no js/wasm-tagged files; nothing to gate'; \
	else \
		echo '--- building in the js/wasm build context'; \
		CGO_ENABLED=0 GOOS=js GOARCH=wasm go build ./...; \
	fi

lint: ## Run golangci-lint, in the host context and again for js/wasm
	command -v golangci-lint || go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
	golangci-lint run
	@if grep -rlq '^//go:build js' --include='*.go' --exclude-dir=vendor . 2>/dev/null; then \
		echo '--- again in the js/wasm build context'; \
		CGO_ENABLED=0 GOOS=js GOARCH=wasm golangci-lint run; \
	fi
