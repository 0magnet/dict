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

.PHONY: demo demo-tinygo serve clean

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
