.PHONY: all
all: build lint test

.PHONY: test
test:
	$(info #Running tests...)
	go clean -testcache
	go test ./...

.PHONY: build
build:
	$(info #Building...)
	go clean -cache
	go build ./...
# ifneq ($(OS),Windows_NT)
	# GOOS=windows GOARCH=amd64 go build ./...
# endif

.PHONY: build-bin
build-bin:
	$(info #Building all Platforms...)
	go clean -cache
	GOOS=windows GOARCH=amd64 go build -o bin/win/translate-proxy.exe ./...
	GOOS=linux GOARCH=amd64 go build -o bin/linux/translate-proxy ./...
	GOOS=darwin GOARCH=amd64 go build -o bin/mac/intel/translate-proxy ./...
	GOOS=darwin GOARCH=arm64 go build -o bin/mac/arm/translate-proxy ./...

.PHONY: build-docker
build-docker:
	$(info #Building linux binary for Docker container)
	docker rmi translate-proxy
	go clean -cache
	GOOS=linux GOARCH=amd64 go build -o docker/translate-proxy ./...
	docker build --tag translate-proxy ./docker


.PHONY: lint
lint:
	$(info #Lints...)
	go install golang.org/x/tools/cmd/goimports@latest
	goimports -w .
	# go vet ./...
	# go install github.com/tetafro/godot/cmd/godot@latest
	# godot ./:
	go install github.com/kisielk/errcheck@latest
	errcheck ./...
	go install github.com/alexkohler/nakedret@latest
	nakedret ./...
	go install golang.org/x/lint/golint@latest
	golint ./...

.PHONY: debug
debug: 	
	dlv debug --headless --listen=:2345 --log --api-version=2