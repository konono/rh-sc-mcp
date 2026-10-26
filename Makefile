BINARY := rh-sc-mcp

DEPLOY := scripts/deploy-openshift.sh
UNINSTALL := scripts/uninstall.sh

.PHONY: build test fmt vet lint install clean \
	deploy-image deploy-plan deploy deploy-status \
	uninstall-stdio uninstall-http uninstall-binary \
	uninstall-podman uninstall-openshift uninstall-credentials

build:
	go build -o bin/$(BINARY) ./cmd/$(BINARY)

test:
	go test -race ./...

fmt:
	gofmt -l -w ./cmd ./internal

vet:
	go vet ./...

lint:
	golangci-lint run

install:
	go install ./cmd/$(BINARY)

# .deploy-tmp is deliberately not removed here. A deploy running in
# another shell owns a directory under it and cleans up after itself;
# `make clean` would pull the rendered manifests out from under it.
clean:
	rm -rf bin

# OpenShift deployment. NAMESPACE and IMAGE have no defaults on purpose:
# both name something that gets written to, and a default would pick one
# for you. Tokens come from ~/.config/rh-sc-mcp (override the directory
# with RH_SC_MCP_CREDENTIAL_DIR), never from arguments.
#
#   make deploy-image  IMAGE=registry/ns/rh-sc-mcp:v0.3.1 ARCH=amd64
#   make deploy-plan   NAMESPACE=rh-sc-mcp IMAGE=registry/ns/rh-sc-mcp:v0.3.1
#   make deploy        NAMESPACE=rh-sc-mcp IMAGE=registry/ns/rh-sc-mcp:v0.3.1
#   make deploy-status NAMESPACE=rh-sc-mcp
#
# Nothing here deletes anything: apply runs without --prune.

deploy-image:
	$(DEPLOY) image

deploy-plan:
	$(DEPLOY) plan

deploy:
	$(DEPLOY) apply

deploy-status:
	$(DEPLOY) status

# Uninstall, one surface at a time. There is no target that removes
# everything: the four deployments leave different things behind, and
# some of them are shared.
#
# Kept unless you name them: the tokens in ~/.config/rh-sc-mcp, the
# OpenShift namespace, the go-installed binary, and any running process.
#
#   make uninstall-stdio                              [SCOPE=user]
#   make uninstall-http                               [SCOPE=user] [URL=] [PORT=]
#   make uninstall-binary       CONFIRM=<path>
#   make uninstall-podman       CONTAINER=rh-sc-mcp   [IMAGE=...]
#   make uninstall-openshift    NAMESPACE=rh-sc-mcp   [KEEP_SECRET=1]
#   make uninstall-credentials  CONFIRM=<dir>

uninstall-stdio:
	$(UNINSTALL) stdio

uninstall-http:
	$(UNINSTALL) http

uninstall-binary:
	$(UNINSTALL) binary

uninstall-podman:
	$(UNINSTALL) podman

uninstall-openshift:
	$(UNINSTALL) openshift

uninstall-credentials:
	$(UNINSTALL) credentials
