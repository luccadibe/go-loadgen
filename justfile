default:
    @just --list

run-example protocol component:
    go run examples/{{protocol}}/{{component}}/main.go