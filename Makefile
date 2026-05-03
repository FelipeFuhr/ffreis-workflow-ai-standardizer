BINARY := standardizer
BIN_DIR := bin

.PHONY: build test lint fmt fmt-check clean

build:
	go build -o $(BIN_DIR)/$(BINARY) ./cmd/standardizer

test:
	go test ./...

lint:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	@diff=$$(gofmt -l .); if [ -n "$$diff" ]; then echo "unformatted files:\n$$diff"; exit 1; fi

clean:
	rm -rf $(BIN_DIR) output/

lefthook:
	./scripts/bootstrap_lefthook.sh
