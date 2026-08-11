.PHONY: frontend
frontend:
	cd web && npm ci && npm run build

.PHONY: test
test: frontend
	go test ./... -count=1

.PHONY: vet
vet:
	go vet ./...

.PHONY: ci
ci: frontend test vet
	go test -race ./... -count=1
