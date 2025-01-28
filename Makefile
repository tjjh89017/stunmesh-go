
APP=stunmesh-go

.PHONY build
build: clean
	go build -v -o ${APP}

.PHONY clean
clean:
	go clean

.PHONY all
all: build
