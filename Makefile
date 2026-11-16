.PHONY: install build test vet uninstall

# Installs `blackbox` and `bbox` into $(go env GOBIN), or $(go env GOPATH)/bin.
install:
	go install ./cmd/blackbox ./cmd/bbox
	@echo "installed blackbox and bbox to $$(go env GOBIN GOPATH | head -n1)"

build:
	go build -o bin/ ./cmd/blackbox ./cmd/bbox

test:
	go vet ./...
	go test ./...

uninstall:
	go clean -i ./cmd/blackbox ./cmd/bbox
