.PHONY: build web stdio test lint clean

build: web stdio

web:
	go build -o lite-dev-agent-web ./cmd/web

stdio:
	go build -o lite-dev-agent .

test:
	go test ./... -v -count=1
lint:
	go vet ./...

clean:
	rm -f lite-dev-agent lite-dev-agent-web lite-dev-agent-test
