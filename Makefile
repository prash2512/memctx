.PHONY: build install clean

build:
	go build -o memctx .

install:
	go install .

clean:
	rm -f memctx

