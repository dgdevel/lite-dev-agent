.PHONY: build test lint clean gui build-gui

build:
	go build -o lite-dev-agent .

test:
	go test ./... -v -count=1

lint:
	go vet ./...

gui: build build-gui

build-gui:
	cd cmd/gui && go mod tidy && go build -o ../../lite-dev-agent-gui .

clean:
	rm -f lite-dev-agent lite-dev-agent-gui lite-dev-agent-test
