GO=GO15VENDOREXPERIMENT="1" CGO_ENABLED=0 GO111MODULE=on go
GOBUILD=$(GO) build

default: build

all: build

build: bank

bank:
	$(GOBUILD) -o bin/bank *.go

# Build for Linux (cross-compilation)
linux: bank-linux

bank-linux:
	@mkdir -p bin
	GOOS=linux GOARCH=amd64 $(GOBUILD) -o bin/bank-linux *.go

# Build for Linux ARM64
linux-arm64:
	@mkdir -p bin
	GOOS=linux GOARCH=arm64 $(GOBUILD) -o bin/bank-linux-arm64 *.go

# Build for Linux ARM (32-bit)
linux-arm:
	@mkdir -p bin
	GOOS=linux GOARCH=arm $(GOBUILD) -o bin/bank-linux-arm *.go

# Build for all Linux architectures (amd64 and ARM64)
# Note: ARM 32-bit is excluded due to dependency compatibility issues
linux-all: bank-linux linux-arm64

.PHONY: bank bank-linux linux linux-arm64 linux-arm linux-all
