.PHONY: build run test test-unit test-integration test-e2e test-all clean docker-up docker-down

build:
	go build -o bin/server ./cmd/server

run:
	go run ./cmd/server -f etc/config.yaml

test-unit:
	go test ./internal/cache/... ./internal/engine/position/... \
	  ./internal/engine/liquidation/... ./internal/tutorial/... \
	  ./pkg/jwt/... -v -count=1

test-integration:
	go test ./internal/model/... ./internal/engine/pricefeed/... \
	  -tags=integration -v -count=1

test-e2e:
	go test ./test/e2e/... -tags=e2e -v -count=1

test-all:
	go test ./... -v -count=1 -coverprofile=coverage.out
	go tool cover -html=coverage.out -o coverage.html

test: test-unit

clean:
	rm -rf bin/ coverage.out coverage.html

docker-up:
	cd deploy && docker-compose up -d

docker-down:
	cd deploy && docker-compose down

db-init:
	psql -h localhost -U postgres -d learn_future -f scripts/init_db.sql
