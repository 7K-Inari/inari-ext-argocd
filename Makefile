.PHONY: build test vet lint ui-build ui-test e2e e2e-kind

build:
	go build ./...

test:
	go test ./...

vet:
	go vet ./...

lint: vet
	gofmt -l .

ui-build:
	npm --prefix ui ci
	npm --prefix ui run build

ui-test:
	npm --prefix ui test

# In-process round trip (no Docker required).
e2e:
	go test ./e2e/...

# Real-cluster round trip: kind + ArgoCD + agentstub. Requires docker + kind.
e2e-kind:
	./e2e/kind/run.sh
