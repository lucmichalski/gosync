all: deps
	@echo "Building."
	go install
deps:
	@echo "Getting Dependencies."
	go get -d -v ./...
fmt:
	@echo "Formatting."
	gofmt -w .
