TEST_DATABASE_URL ?= postgres://postgres:postgres@127.0.0.1:5432/projects_service?sslmode=disable
GO_TEST_ENV = GOCACHE=/tmp/go-build GOMODCACHE=/tmp/go-mod-cache

.PHONY: test test-unit test-integration

test:
	@set -e; \
	trap 'docker compose stop postgres' EXIT; \
	docker compose up -d postgres; \
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' $(GO_TEST_ENV) go test -p 1 ./...

test-unit:
	$(GO_TEST_ENV) go test ./...

test-integration:
	TEST_DATABASE_URL='$(TEST_DATABASE_URL)' $(GO_TEST_ENV) go test -p 1 ./...
