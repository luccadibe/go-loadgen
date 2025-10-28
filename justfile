default:
    @just --list

run-example protocol component:
    go run examples/{{protocol}}/{{component}}/main.go

test:
    go test -v ./...

test-coverage:
    go test -v -coverprofile=coverage.out ./...
    go tool cover -html=coverage.out -o coverage.html

test-race:
    go test -v -race ./...

bench:
    go test -v -bench=. ./...

tidy:
    go mod tidy

tag version:
    git tag -a v{{version}} -m "Release v{{version}}"
    git push origin v{{version}}