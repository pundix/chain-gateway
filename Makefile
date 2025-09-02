.PHONY: deploy

deploy:
	./scripts/deploy.sh

.PHONY: init

init:
	./scripts/init.sh


BUILDDIR ?= $(CURDIR)/build
CMD := cgc
PROGRAM := ./client/
install:
	@go build -mod=readonly -ldflags $(ldflags) -v -o $(BUILDDIR)/bin/$(CMD) $(PROGRAM)
	cp ./build/bin/$(CMD) /usr/local/bin
	rm -rf ./build/bin

