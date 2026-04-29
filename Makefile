.PHONY: build web test lint clean

build: web

web:
	go build -o lite-dev-agent-web ./cmd/web

test:
	go test ./... -v -count=1

lint:
	go vet ./...

clean:
	rm -f lite-dev-agent lite-dev-agent-web lite-dev-agent-test
