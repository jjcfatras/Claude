BIN := bin/code-review-helper
PLUGIN_BIN := ../../bin
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64

.PHONY: build release test fmt vet clean

# Local dev build (single binary, host platform).
build:
	go build -o $(BIN) ./cmd/helper

# Cross-compile prebuilts for every supported platform into the plugin's bin/.
# Run this before tagging a new plugin version.
release:
	@for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; \
	  out=$(PLUGIN_BIN)/code-review-helper-$${os}-$${arch}; \
	  echo "→ $$out"; \
	  GOOS=$$os GOARCH=$$arch go build -o $$out ./cmd/helper; \
	  chmod +x $$out; \
	done

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -rf bin
