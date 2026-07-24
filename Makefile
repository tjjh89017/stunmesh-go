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
ALL_BUILTINS := builtin_cloudflare builtin_opendht
# Empty selects the per-platform default from the internal/wg build constraints:
# wgcli on freebsd, wgctrl elsewhere. Set to override.
BACKEND ?=
PREFIX ?= /usr/local
BINDIR ?= $(PREFIX)/bin

# Validate BACKEND value. filter-out rejects unknown words, and the word count
# rejects a contradictory 'wgctrl wgcli', which filter alone would accept.
ifneq ($(BACKEND),)
    ifneq ($(filter-out wgctrl wgcli,$(BACKEND))$(filter-out 1,$(words $(BACKEND))),)
        $(error BACKEND must be 'wgctrl' or 'wgcli', got '$(BACKEND)')
    endif
endif

# Platforms whose wgctrl backend requires CGO_ENABLED=1. They default to wgcli,
# so this only applies when wgctrl is explicitly requested.
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

# Combine BUILTIN and BACKEND tags. An empty BACKEND adds no tag, leaving the
# backend choice to the build constraints in internal/wg.
ALL_TAGS := $(strip $(BUILTIN) $(BACKEND))
TAGS_FLAGS = $(if $(ALL_TAGS),-tags '$(ALL_TAGS)',)

UPX_TARGET =
ifneq ($(UPX),0)
	UPX_TARGET = upx
endif

# Set CGO_ENABLED: only an explicit wgctrl on CGO_REQUIRED_PLATFORMS needs it,
# so every default build is CGO-free.
ifeq ($(BACKEND),wgctrl)
	ifeq ($(GOOS),$(filter $(GOOS),$(CGO_REQUIRED_PLATFORMS)))
		CGO_ENABLED = 1
		LDFLAGS := ${LDFLAGS} -extldflags="-static"
	else
		CGO_ENABLED = 0
	endif
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

# Platforms to lint. golangci-lint only sees the files its GOOS selects, and
# the darwin/bsd STUN implementation is the most platform-specific code here,
# so linting the host alone leaves it unchecked. Build tags come from
# .golangci.yaml so that a bare golangci-lint run agrees with this.
LINT_PLATFORMS ?= linux darwin freebsd

.PHONY: lint
lint:
	@for os in $(LINT_PLATFORMS); do \
		echo "Linting GOOS=$$os..."; \
		GOOS=$$os golangci-lint run || exit 1; \
	done

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
