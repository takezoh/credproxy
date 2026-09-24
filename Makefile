BINARY := credproxyd
RUNNER := credproxy
OUTPUT_DIR := bin

.PHONY: build test vet lint install clean

build:
	mkdir -p $(OUTPUT_DIR)
	go build -o $(OUTPUT_DIR)/$(BINARY) ./cmd/credproxyd
	go build -o $(OUTPUT_DIR)/$(RUNNER) ./cmd/credproxy

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

install:
	bash ./install.sh

clean:
	rm -rf $(OUTPUT_DIR)
