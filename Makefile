.PHONY: build test test-go test-py offline docker clean

build:
	go build -o bin/azlocal ./cmd/azlocal

test: test-go

test-go:
	go test ./...

test-py: build
	./bin/azlocal start --daemon --port 4577
	@trap './bin/azlocal stop || true' EXIT; \
	eval "$$(./bin/azlocal env)" && \
	pip install -q -r tests/python/requirements.txt && \
	pytest -q tests/python

offline:
	bash scripts/offline-test.sh

docker:
	docker build -f docker/Dockerfile -t azure-local:dev .

clean:
	rm -rf bin
