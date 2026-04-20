# require GNU Make

APP ?= stunmesh-go
GO_FLAGS ?=
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
STRIP ?= 0
TRIMPATH ?= 0
UPX ?= 0
EXTRA_MIN ?= 0
BUILTIN ?= all
ALL_BUILTINS := builtin_cloudflare
BACKEND ?= ctrl
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

# Validate BACKEND value
ifeq ($(filter $(BACKEND),ctrl cli),)
    $(error BACKEND must be 'ctrl' or 'cli', got '$(BACKEND)')
endif

# Platforms that require CGO_ENABLED=1 (only ctrl backend on freebsd)
CGO_REQUIRED_PLATFORMS := freebsd

ifneq ($(EXTRA_MIN),0)
	STRIP = 1
	TRIMPATH = 1
	UPX = 1
endif

LDFLAGS =
ifneq ($(STRIP),0)
	LDFLAGS := -s -w
endif

TRIMPATH_FLAGS =
ifneq ($(TRIMPATH),0)
	TRIMPATH_FLAGS := -trimpath
endif

# Expand BUILTIN=all to all available built-in plugins
ifeq ($(BUILTIN),all)
	override BUILTIN := $(ALL_BUILTINS)
endif

# Backend build tag: wgcli selects the wg-CLI shelling backend
BACKEND_TAG :=
ifeq ($(BACKEND),cli)
	BACKEND_TAG := wgcli
endif

# Combine BUILTIN and BACKEND tags
ALL_TAGS := $(strip $(BUILTIN) $(BACKEND_TAG))
TAGS_FLAGS = $(if $(ALL_TAGS),-tags '$(ALL_TAGS)',)

UPX_TARGET =
ifneq ($(UPX),0)
	UPX_TARGET = upx
endif

# Set CGO_ENABLED: forced off when BACKEND=cli; otherwise platform default
ifeq ($(BACKEND),cli)
	CGO_ENABLED = 0
else ifeq ($(GOOS),$(filter $(GOOS),$(CGO_REQUIRED_PLATFORMS)))
	CGO_ENABLED = 1
	LDFLAGS := ${LDFLAGS} -extldflags="-static"
else
	CGO_ENABLED = 0
endif

GO_FLAGS := ${GO_FLAGS} -ldflags '${LDFLAGS}' ${TRIMPATH_FLAGS} ${TAGS_FLAGS}

.PHONY: all
all: clean build $(UPX_TARGET)

.PHONY: build
build:
	CGO_ENABLED=${CGO_ENABLED} GOOS=${GOOS} GOARCH=${GOARCH} go build ${GO_FLAGS} -v -o ${APP}

.PHONY: upx
upx:
	upx --lzma --best ${APP}


.PHONY: clean
clean:
	go clean

.PHONY: test
test:
	go test -cover -v ${TAGS_FLAGS} ./...

.PHONY: fmt
fmt:
	go fmt ./...

.PHONY: vet
vet:
	go vet ${TAGS_FLAGS} ./...

.PHONY: install
install: build
	install -d $(BINDIR)
	install -m 755 $(APP) $(BINDIR)/$(APP)

.PHONY: uninstall
uninstall:
	rm -f $(BINDIR)/$(APP)

.PHONY: plugin
plugin:
	$(MAKE) -C contrib all

.PHONY: contrib
contrib: plugin

.PHONY: plugin-clean
plugin-clean:
	$(MAKE) -C contrib clean

.PHONY: contrib-clean
contrib-clean: plugin-clean

.PHONY: plugin-install
plugin-install:
	$(MAKE) -C contrib install PREFIX=$(PREFIX) BINDIR=$(BINDIR)

.PHONY: contrib-install
contrib-install: plugin-install

.PHONY: plugin-uninstall
plugin-uninstall:
	$(MAKE) -C contrib uninstall PREFIX=$(PREFIX) BINDIR=$(BINDIR)

.PHONY: contrib-uninstall
contrib-uninstall: plugin-uninstall
