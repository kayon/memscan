-include .env

SHARED_NAME=memscan.so
HEADER_NAME=$(SHARED_NAME:.so=.h)

DECK_USER=deck
DECK_PASS?=$(DECK_PWD)
DECK_HOST?=$(DECK_HOST)
DECK_PLUG_BACKEND_DIR=/home/deck/homebrew/plugins/reroll/bin


.PHONY: all build build.cross deploy clean
all: build

build:
	@GOOS=linux \
     GOARCH=amd64 \
     CGO_ENABLED=1 \
     go build -trimpath -ldflags="-s -w" -o $(SHARED_NAME) -buildmode=c-shared
	@rm -f $(HEADER_NAME)

build.cross:
	@GOOS=linux \
     GOARCH=amd64 \
     CGO_ENABLED=1 \
     CC="zig cc -target x86_64-linux-gnu" \
     go build -trimpath -ldflags="-s -w" -o $(SHARED_NAME) -buildmode=c-shared
	@rm -f $(HEADER_NAME)

deploy:
	@sshpass -p $(DECK_PASS) rsync -avz --rsync-path="sudo rsync" ./$(SHARED_NAME) $(DECK_USER)@$(DECK_HOST):$(DECK_PLUG_BACKEND_DIR)

clean:
	@rm -f ./$(SHARED_NAME)
	@rm -f ./$(HEADER_NAME)
