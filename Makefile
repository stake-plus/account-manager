# Simple cross-platform Makefile for Account Monitor

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOMOD=$(GOCMD) mod
GOGET=$(GOCMD) get

# Binary names
BINARY_NAME=account-monitor
BINARY_DIR=bin

# Detect OS for binary extension
ifeq ($(OS),Windows_NT)
    BINARY_EXT=.exe
    RM=cmd /C del /Q /F
    RRM=cmd /C rmdir /Q /S
    MKDIR=cmd /C mkdir
    SLASH=\\
    NULL= 2>nul || (exit 0)
else
    BINARY_EXT=
    RM=rm -f
    RRM=rm -rf
    MKDIR=mkdir -p
    SLASH=/
    NULL= 2>/dev/null || true
endif

BINARY=$(BINARY_DIR)$(SLASH)$(BINARY_NAME)$(BINARY_EXT)

# Source path
MAIN_PATH=src/account-monitor/main.go

.PHONY: all build clean deps tidy

# Default target
all: tidy deps build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	@$(MKDIR) $(BINARY_DIR) $(NULL)
	$(GOBUILD) -o $(BINARY) -v $(MAIN_PATH)
	@echo "Build complete: $(BINARY)"

# Update dependencies
deps:
	@echo "Updating dependencies..."
	$(GOGET) -u ./...
	@echo "Dependencies updated"

# Tidy modules
tidy:
	@echo "Tidying modules..."
	$(GOMOD) tidy
	@echo "Modules tidied"

# Clean build artifacts
clean:
	@echo "Cleaning..."
	$(RM) $(BINARY_DIR)$(SLASH)$(BINARY_NAME)* $(NULL)
	@echo "Clean complete"