.PHONY: build run clean test lint build-linux build-linux-arm64 build-windows build-all midi-train

APP_NAME := boom
BUILD_DIR := build
GO := go

# Build flags
LDFLAGS := -s -w

build:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME) ./cmd/boom

run: build
	./$(BUILD_DIR)/$(APP_NAME)

clean:
	rm -rf $(BUILD_DIR)

test:
	$(GO) test ./...

lint:
	golangci-lint run ./...

# Cross-compilation targets
build-linux:
	GOOS=linux GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME)-linux-amd64 ./cmd/boom

build-linux-arm64:
	GOOS=linux GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME)-linux-arm64 ./cmd/boom

build-windows:
	GOOS=windows GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME)-windows-amd64.exe ./cmd/boom

midi-train:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/midi-train ./cmd/midi-train

build-all: build build-linux build-linux-arm64 build-windows
