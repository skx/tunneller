#!/bin/sh

# Install the lint-tool, and the shadow-tool
go get -u golang.org/x/lint/golint
go get -u golang.org/x/tools/go/analysis/passes/shadow/cmd/shadow

# At this point failures cause aborts
set -e

# Run the linter
echo "Launching linter .."
golint -set_exit_status ./...
echo "Completed linter .."

# Run the shadow-checker
echo "Launching shadowed-variable check .."
go vet -vettool=$(which shadow) ./...
echo "Completed shadowed-variable check .."

# Run golang tests
go test ./...
