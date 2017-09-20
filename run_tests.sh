#!/usr/bin/env bash
set -ex

# The script does automatic checking on a Go package and its sub-packages,
# including:
# 1. gofmt         (http://golang.org/cmd/gofmt/)
# 2. go vet        (http://golang.org/cmd/vet)
# 3. gosimple      (https://github.com/dominikh/go-simple)
# 4. unconvert     (https://github.com/mdempsky/unconvert)
# 5. ineffassign   (https://github.com/gordonklaus/ineffassign)
# 6. race detector (http://blog.golang.org/race-detector)

# gometalinter (github.com/alecthomas/gometalinter) is used to run each each
# static checker.

# To run on docker on windows, symlink /mnt/c to /c and then execute the script
# from the repo path under /c.  See:
# https://github.com/Microsoft/BashOnWindows/issues/1854
# for more details.

#Default GOVERSION
GOVERSION=${1:-1.9}
REPO=dcrstakepool

TESTDIRS=$(go list ./... | grep -v '/vendor/')
TESTCMD="test -z \"\$(gometalinter --vendor --disable-all \
  --enable=gofmt \
  --enable=vet \
  --enable=gosimple \
  --enable=unconvert \
  --enable=ineffassign \
  --deadline=10m ./... | tee /dev/stderr)\" && \
  env GORACE='halt_on_error=1' go test -short -race \
  \${TESTDIRS}"

if [ $GOVERSION == "local" ]; then
    eval $TESTCMD
    exit
fi

DOCKER_IMAGE_TAG=decred-golang-builder-$GOVERSION

docker pull decred/$DOCKER_IMAGE_TAG

docker run --rm -it -v $(pwd):/src decred/$DOCKER_IMAGE_TAG /bin/bash -c "\
  rsync -ra --filter=':- .gitignore'  \
  /src/ /go/src/github.com/decred/$REPO/ && \
  cd github.com/decred/$REPO/ && \
  dep ensure && \
  go install . && \
  $TESTCMD
"

echo "------------------------------------------"
echo "Tests complete."
