test: clean
	go test -cover -v ./...

deps:
	go get -v -u launchpad.net/gocheck
	go get -v -u github.com/mailgun/minheap
	go get -v -u github.com/mailgun/timetools

clean:
	find . -name flymake_* -delete

msloccount:
	 find . -name "*.go" -print0 | xargs -0 wc -l
