BINARY := credproxyd
BIN_DIR := /usr/local/bin

.PHONY: build test vet lint install clean

build:
	go build -o $(BINARY) ./cmd/credproxyd

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

install: build
	install -m 0755 $(BINARY) $(BIN_DIR)/$(BINARY)
	@echo "Installed $(BIN_DIR)/$(BINARY)"

clean:
	rm -f $(BINARY)
