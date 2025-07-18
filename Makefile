BINARY_NAME=gopod
DIST_DIR=dist

# Default: build for current platform
all: build

build:
	go build -o $(BINARY_NAME) .

build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 .

build-linux-arm64:
	GOOS=linux GOARCH=arm64 go build -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 .

build-windows-amd64:
	GOOS=windows GOARCH=amd64 go build -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe .

build-windows-arm64:
	GOOS=windows GOARCH=arm64 go build -o $(DIST_DIR)/$(BINARY_NAME)-windows-arm64.exe .

build-darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 .

release: clean
	mkdir -p $(DIST_DIR)
	$(MAKE) build-linux-amd64
	$(MAKE) build-linux-arm64
	$(MAKE) build-windows-amd64
	$(MAKE) build-windows-arm64
	$(MAKE) build-darwin-arm64

test:
	go test ./...

clean:
	rm -rf $(BINARY_NAME) $(DIST_DIR)

.PHONY: all build build-linux-amd64 build-linux-arm64 build-windows-amd64 build-windows-arm64 build-darwin-arm64 release test clean
