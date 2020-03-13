#!/bin/bash
# The script does automatic checking on a Go package and its sub-packages, including:
# 1. gofmt         (http://golang.org/cmd/gofmt/)
# 2. go vet        (http://golang.org/cmd/vet)
# 3. gosimple      (https://github.com/dominikh/go-simple)
# 4. unconvert     (https://github.com/mdempsky/unconvert)
# 5. ineffassign   (https://github.com/gordonklaus/ineffassign)
# 6. race detector (http://blog.golang.org/race-detector)
# 7. test coverage (http://blog.golang.org/cover)

set -ex

dir=$(pwd)
# list of all modules to test
modules=". backend/stakepoold/rpc/client/dcrwallet"

# Test each module separately.
for m in $modules
do
	cd $dir/$m
	# run tests
	env GORACE="halt_on_error=1" go test -tags live -race -short ./...
done

# Return to initial directory.
cd $dir

# golangci-lint (github.com/golangci/golangci-lint) is used to run each each
# static checker.

# check linters
golangci-lint run --disable-all --deadline=10m \
  --enable=gofmt \
  --enable=vet \
  --enable=gosimple \
  --enable=unconvert \
  --enable=ineffassign
