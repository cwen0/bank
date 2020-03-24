GO=GO15VENDOREXPERIMENT="1" CGO_ENABLED=0 GO111MODULE=on go
GOBUILD=$(GO) build

default: build

all: build

build: bank

bank:
	$(GOBUILD) -o bin/bank *.go

.PHONY: bank
