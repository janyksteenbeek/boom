.PHONY: build run clean test lint \
	build-linux build-linux-arm64 build-windows build-all \
	package package-darwin-amd64 package-darwin-arm64 package-linux-amd64 package-linux-arm64 package-windows package-all \
	midi-train

APP_NAME := boom
BUILD_DIR := build
GO := go
FYNE := $(shell go env GOPATH)/bin/fyne

FYNE_PKG := $(FYNE) package -src ./cmd/boom

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

# Cross-compilation targets (raw binaries)
build-linux:
	GOOS=linux GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME)-linux-amd64 ./cmd/boom

build-linux-arm64:
	GOOS=linux GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME)-linux-arm64 ./cmd/boom

build-windows:
	GOOS=windows GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(APP_NAME)-windows-amd64.exe ./cmd/boom

build-all: build build-linux build-linux-arm64 build-windows

# Packaged app bundles (uses fyne tool + FyneApp.toml metadata)
# Install fyne CLI: go install fyne.io/fyne/v2/cmd/fyne@latest

package-darwin-arm64:
	@mkdir -p $(BUILD_DIR)
	GOARCH=arm64 $(FYNE_PKG) -os darwin
	mv Boom.app $(BUILD_DIR)/Boom-darwin-arm64.app
	./scripts/bundle-portaudio-darwin.sh $(BUILD_DIR)/Boom-darwin-arm64.app

package-darwin-amd64:
	@mkdir -p $(BUILD_DIR)
	GOARCH=amd64 $(FYNE_PKG) -os darwin
	mv Boom.app $(BUILD_DIR)/Boom-darwin-amd64.app
	./scripts/bundle-portaudio-darwin.sh $(BUILD_DIR)/Boom-darwin-amd64.app

package-linux-amd64:
	@mkdir -p $(BUILD_DIR)
	GOARCH=amd64 $(FYNE_PKG) -os linux
	mv Boom.tar.xz $(BUILD_DIR)/Boom-linux-amd64.tar.xz

package-linux-arm64:
	@mkdir -p $(BUILD_DIR)
	GOARCH=arm64 $(FYNE_PKG) -os linux
	mv Boom.tar.xz $(BUILD_DIR)/Boom-linux-arm64.tar.xz

package-windows:
	@mkdir -p $(BUILD_DIR)
	GOARCH=amd64 $(FYNE_PKG) -os windows
	mv Boom.exe $(BUILD_DIR)/Boom-windows-amd64.exe

package: package-darwin-arm64 package-darwin-amd64

package-all: package-darwin-arm64 package-darwin-amd64 package-linux-amd64 package-linux-arm64 package-windows

midi-train:
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/midi-train ./cmd/midi-train
