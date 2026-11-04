GO ?= go
export GOCACHE ?= /tmp/aginx-go-cache
export GOMODCACHE ?= /tmp/aginx-go-modcache

.PHONY: build test race vet check fuzz bench run
build:
	mkdir -p bin
	CGO_ENABLED=0 $(GO) build -buildvcs=false -trimpath -ldflags='-s -w' -o bin/aginx ./cmd/aginx
test:
	$(GO) test ./...
race:
	CGO_ENABLED=1 $(GO) test -race ./...
vet:
	$(GO) vet ./...
check: vet test race build
fuzz:
	$(GO) test ./internal/http1 -run='^$$' -fuzz=FuzzRead -fuzztime=10s -parallel=2
	$(GO) test ./internal/ginxaccess -run='^$$' -fuzz=FuzzParse -fuzztime=10s -parallel=2
bench:
	$(GO) test ./internal/ginxaccess -run='^$$' -bench=. -benchmem
run: build
	./bin/aginx -root ./examples/www
