# require GNU Make

APP?=stunmesh-go
GO_FLAGS?=
GOOS?=$(shell go env GOOS)
STRIP?=0

# Platforms that require CGO_ENABLED=1
CGO_REQUIRED_PLATFORMS := freebsd openbsd

LDFLAGS=
ifneq ($(STRIP),0)
	LDFLAGS:="-s -w"
endif

# Set CGO_ENABLED based on OS
ifeq ($(GOOS),$(filter $(GOOS),$(CGO_REQUIRED_PLATFORMS)))
	CGO_ENABLED=1
	GO_FLAGS:=${GO_FLAGS} -ldflags ${LDFLAGS} -extldflags="-static"
else
	CGO_ENABLED=0
	GO_FLAGS:=${GO_FLAGS} -ldflags ${LDFLAGS}
endif

.PHONY: build
build: clean
	CGO_ENABLED=${CGO_ENABLED} go build ${GO_FLAGS} -v -o ${APP}

.PHONY: clean
clean:
	go clean

.PHONY: all
all: build
