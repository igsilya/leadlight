.PHONY: all tidy audit test build install

all: tidy audit test build

tidy:
	go fmt ./...
	go mod tidy

audit:
	go vet -all ./...

test:
	go test ./...

build:
	go build -o leadlight .

install:
	go install .
