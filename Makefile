.PHONY: build test lint clean

build:
	go build -o lite-dev-agent .

test:
	go test ./... -v -count=1
lint:
	go vet ./...

clean:
	rm -f lite-dev-agent
