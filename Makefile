.PHONY: test lint demo

# -count=1 because the integration tests build the binary themselves, so
# the test cache can't see changes to plugin/scripts.
test:
	test -z "$$(gofmt -l plugin examples tests)"
	go vet ./...
	go test -race -count=1 ./...

lint:
	claude plugin validate ./plugin --strict
	claude plugin validate . --strict

# The golang-migrate and dbmate examples have problems on purpose and must
# exit 1. The goose example is fine and must exit 0. The binary is built
# first because go run turns every failing exit code into 1.
demo:
	go build -o neverapplied ./plugin/scripts
	./neverapplied --base examples/golang-migrate/base examples/golang-migrate/head || test $$? -eq 1
	./neverapplied --base examples/dbmate/base examples/dbmate/head || test $$? -eq 1
	./neverapplied --base examples/goose/base examples/goose/head
