codegen:
	sqlc generate
	go generate tools/tools.go

build-server:
	go build -o ./bin/server ./cmd/server/server.go

build-worker:
	go build -o ./bin/worker ./cmd/worker/worker.go

build: build-server build-worker

run-server: build-server
	./bin/server

run-worker: build-worker
	./bin/worker

test:
	go test -v ./...
