GO ?= go
GOCACHE ?= /tmp/memy-go-build
GOLANGCI_LINT_CACHE ?= /tmp/memy-golangci-cache
GOLANGCI_LINT ?= golangci-lint
RELEASE_TYPE ?= patch
TEST_TIMEOUT ?= 30m
TEST_FLAGS ?= -v -race -count=1 -timeout=$(TEST_TIMEOUT)
FUZZTIME ?= 30s
FUZZPARALLEL ?= 2
MODULES := $(shell find . -type d \( -name ".*" -not -name "." -o -name "vendor" \) -prune -o -type f -name "go.mod" -exec dirname {} \;)
export GOCACHE GOLANGCI_LINT_CACHE

.PHONY: format vet lint fix test race validate examples bench fuzz cover release release-patch release-break release-test consumer-local consumer-published

format:
	@test -z "$$(gofmt -l .)"

vet:
	@for dir in $(MODULES); do \
		(cd "$$dir" && $(GO) vet ./...) || exit 1; \
	done

lint:
	@for dir in $(MODULES); do \
		echo "golangci-lint - $$dir"; \
		(cd "$$dir" && $(GOLANGCI_LINT) run --allow-serial-runners ./...) || exit 1; \
	done

fix:
	@if [ -f "go.work" ]; then $(GO) work sync; fi
	@for dir in $(MODULES); do \
		echo "fix & tidy - $$dir"; \
		(cd "$$dir" && $(GO) fix ./... && $(GO) mod tidy) || exit 1; \
		(cd "$$dir" && $(GOLANGCI_LINT) run --fix --allow-serial-runners ./...) || exit 1; \
	done

test:
	@for dir in $(MODULES); do \
		echo "test - $$dir"; \
		(cd "$$dir" && $(GO) test $(TEST_FLAGS) ./...) || exit 1; \
	done

race: test

validate: format vet lint test examples release-test

examples:
	$(GO) run ./examples/lifecycle
	$(GO) run ./examples/quality
	$(GO) run ./examples/retrieval
	$(GO) run ./examples/quality-integration
	$(GO) run ./examples/managed-projections

bench:
	@for dir in $(MODULES); do \
		echo "bench - $$dir"; \
		(cd "$$dir" && $(GO) test -bench=. -run='^$$' -benchmem ./...) || exit 1; \
	done

fuzz:
	@for dir in $(MODULES); do \
		echo "fuzz - $$dir"; \
		(cd "$$dir" && \
			packages=$$($(GO) list -tags=fuzz ./...) && \
			for pkg in $$packages; do \
				targets=$$($(GO) test -tags=fuzz -list '^Fuzz' "$$pkg") || exit 1; \
				for target in $$targets; do \
					case "$$target" in Fuzz*) \
						$(GO) test -tags=fuzz -run='^$$' -fuzz="^$$target$$" \
							-fuzztime=$(FUZZTIME) -parallel=$(FUZZPARALLEL) "$$pkg" || exit 1 ;; \
					esac; \
				done; \
			done \
		) || exit 1; \
	done

cover:
	@for dir in $(MODULES); do \
		echo "cover - $$dir"; \
		(cd "$$dir" && $(GO) test -coverprofile=coverage.out ./... && $(GO) tool cover -func=coverage.out) || exit 1; \
	done

release-test:
	python3 scripts/release_test.py

release: validate
	./scripts/release.sh "$(RELEASE_TYPE)" "$(MODULES)"

release-patch: validate
	./scripts/release.sh patch "$(MODULES)"

release-break: validate
	./scripts/release.sh break "$(MODULES)"

# Optional external composition; not a dependency of offline core validation.
consumer-local:
	python3 scripts/consumer_checks.py local

consumer-published:
	python3 scripts/consumer_checks.py published --memy-ref "$(MEMY_REF)"
