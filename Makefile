.PHONY: build test lint clean gui

build:
	go build -o lite-dev-agent .
	cd cmd/gui && go mod tidy && go build -o ../../lite-dev-agent-gui .

test:
	go test ./... -v -count=1

lint:
	go vet ./...

clean:
	rm -f lite-dev-agent lite-dev-agent-gui lite-dev-agent-test
