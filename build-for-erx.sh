#!/bin/bash

GOOS=linux GOARCH=mipsle go build -ldflags "-s -w" .
