.PHONY: test
test:
	go test ./... -count=1

.PHONY: vet
vet:
	go vet ./...

.PHONY: ci
ci: test vet
	go test -race ./... -count=1
