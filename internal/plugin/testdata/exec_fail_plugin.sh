#!/bin/sh
# Test exec plugin that always fails

echo "{\"success\":false,\"error\":\"simulated failure\"}"
exit 1
