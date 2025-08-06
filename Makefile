# require GNU Make

APP?=stunmesh-go
GO_FLAGS?=

# Platforms that require CGO_ENABLED=1
CGO_REQUIRED_PLATFORMS := FreeBSD OpenBSD

# Set CGO_ENABLED based on OS
ifeq ($(shell uname -s),$(filter $(shell uname -s),$(CGO_REQUIRED_PLATFORMS)))
	CGO_ENABLED=1
	GO_FLAGS:=${GO_FLAGS} -ldflags -extldflags="-static"
else
	CGO_ENABLED=0
endif

.PHONY: build
build: clean
	CGO_ENABLED=${CGO_ENABLED} go build ${GO_FLAGS} -v -o ${APP}

.PHONY: clean
clean:
	go clean

.PHONY: all
all: build
