.PHONY: build test run-server run-scan clean

GO ?= go
ifeq ($(OS),Windows_NT)
EXEEXT := .exe
endif
CLI := bin/driftctl$(EXEEXT)
SERVER := bin/drift-server$(EXEEXT)

build:
	mkdir -p bin
	$(GO) build -o $(CLI) ./cmd/driftctl
	$(GO) build -o $(SERVER) ./cmd/drift-server

test:
	go test ./...

run-server: build
	./$(SERVER) -config configs/driftctl.yaml

run-scan: build
	./$(CLI) scan --state testdata/state/sample.tfstate --provider aws --skip-cloud

clean:
	rm -rf bin/
