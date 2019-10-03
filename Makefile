.PHONY: build
build:
	@docker run \
		--rm \
		-e CGO_ENABLED=0 \
		-v $(PWD):/usr/src/concord-observer-gitlab \
		-w /usr/src/concord-observer-gitlab \
		golang /bin/sh -c "go get -v -d && go build -a -installsuffix cgo -o main"
	@docker build -t concord/observer-gitlab .
	@rm -f main

.PHONY: test
test:
	@docker run \
		-d \
		-e CONCORD_STATUS_CHANGE_NOTIFIER_HOST=localhost:5555 \
		-v $(PWD)/src:/go/src/concord-observer-gitlab \
		-w /go/src/concord-observer-gitlab \
		--name concord-observer-gitlab_test \
		golang /bin/sh -c "go get -v -t -d && go test -v -coverprofile=.coverage.out"
	@docker logs -f concord-observer-gitlab_test
	@docker rm -f concord-observer-gitlab_test

.PHONY: test-short
test-short:
	@docker run \
		--rm \
		-it \
		-e CONCORD_STATUS_CHANGE_NOTIFIER_HOST=localhost:5555 \
		-v $(PWD)/src:/go/src/concord-observer-gitlab \
		-w /go/src/concord-observer-gitlab \
		golang /bin/sh -c "go get -v -t -d && go test -short -v -coverprofile=.coverage.out"
