# Copyright 2026 Leadlight Authors
# SPDX-License-Identifier: Apache-2.0

.PHONY: all tidy audit test build install vendor

GOFLAGS := -mod=vendor

all: tidy audit test build

tidy:
	go fmt ./...
	go mod tidy

audit:
	go vet -all $(GOFLAGS) ./...

test:
	go test $(GOFLAGS) ./...

build:
	go build $(GOFLAGS) -o leadlight .

install:
	go install $(GOFLAGS) .

vendor:
	go mod vendor
