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
ifneq ($(OS),Windows_NT)
	GOOS=windows GOARCH=amd64 go build ./...
endif

.PHONY: buildbin
buildbin:
	$(info #Building all Platforms...)
	go clean -cache
	GOOS=windows GOARCH=amd64 go build -o bin/translator-proxy.exe ./...
	GOOS=linux GOARCH=amd64 go build -o bin/translator-proxy-linux ./...
	GOOS=darwin GOARCH=amd64 go build -o bin/translator-proxy-mac ./...

.PHONY: zipbin
zipbin: buildbin
	$(info #Zip all Platforms...)
	zip bin/translator-proxy-win.zip bin/translator-proxy.exe
	zip bin/translator-proxy-linux.zip bin/translator-proxy-linux
	zip bin/translator-proxy-mac.zip bin/translator-proxy-mac

.PHONY: lint
lint:
	$(info #Lints...)
	echo ${PATH}
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