-include .env

BINARY_NAME=memscan-cli

DECK_USER=deck
DECK_PASS?=$(DECK_PWD)
DECK_HOST?=$(DECK_HOST)
DECK_DIR=/home/deck/Documents

.PHONY: all build deploy clean
all: build

build:
	@GOOS=linux GOARCH=amd64 go build -o ./$(BINARY_NAME) .

deploy: build
	@sshpass -p $(DECK_PASS) scp ./$(BINARY_NAME) $(DECK_USER)@$(DECK_HOST):$(DECK_DIR)

clean:
	rm -f ./$(BINARY_NAME)