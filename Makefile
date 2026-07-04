.PHONY: all build test lint tidy clean install

all: build

build:
	go build -ldflags="-s -w" -o bin/taxonrouter-mcp ./cmd/taxonrouter-mcp
	go build -ldflags="-s -w" -o bin/taxonrouter-auto-tagger ./cmd/taxonrouter-auto-tagger

test:
	go test -race -v -count=1 ./...

lint:
	go vet ./...
	go mod tidy
	git diff --exit-code

tidy:
	go mod tidy

clean:
	rm -rf bin/

install:
	go install ./cmd/taxonrouter-mcp
	go install ./cmd/taxonrouter-auto-tagger

# Build for Linux x86_64 (useful for self-hosted runners)
build-linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/taxonrouter-mcp ./cmd/taxonrouter-mcp
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/taxonrouter-auto-tagger ./cmd/taxonrouter-auto-tagger

docker-build:
	docker build -t taxonrouter:latest .

docker-push:
	docker push taxonrouter:latest
