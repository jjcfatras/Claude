BIN := bin/code-review-helper
INSTALL_DIR := $(HOME)/.claude/bin

.PHONY: build install test clean fmt vet

build:
	go build -o $(BIN) ./cmd/helper

install: build
	mkdir -p $(INSTALL_DIR)
	install -m 0755 $(BIN) $(INSTALL_DIR)/code-review-helper

test:
	go test ./...

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -rf bin
