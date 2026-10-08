GO ?= go
GOCACHE ?= /tmp/memy-go-build
GOLANGCI_LINT_CACHE ?= /tmp/memy-golangci-cache
GOLANGCI_LINT ?= $(CURDIR)/bin/golangci-lint
GO_VERSION := 1.27.1
LINT_VERSION := 2.14.0
DEVELOPMENT_MODULES := . tools integration/consumer
PUBLISHABLE_MODULES := .
TEST_TIMEOUT ?= 30m
TEST_FLAGS ?= -mod=readonly -race -count=1 -timeout=$(TEST_TIMEOUT)
CHECK_TEST_FLAGS := -mod=readonly -race -count=1 -timeout=$(TEST_TIMEOUT)
FUZZTIME ?= 30s
FUZZPARALLEL ?= 2
MEMY_REF ?= v0.3.1
V ?= 0
ifeq ($(V),1)
Q :=
else
Q := @
endif
export GOWORK := off
export GOCACHE GOLANGCI_LINT_CACHE GO MEMY_REF

.PHONY: install-tools environment inventory print-development-modules print-publishable-modules lint test check examples test-integration test-candidate test-published release-candidate bench fuzz cover release-patch release-break

print-development-modules:
	@echo $(DEVELOPMENT_MODULES)
print-publishable-modules:
	@echo $(PUBLISHABLE_MODULES)

install-tools:
	$(Q)mkdir -p bin
	$(Q)GOBIN="$(CURDIR)/bin" $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(LINT_VERSION)

environment:
	$(Q)test "$$( $(GO) env GOVERSION )" = "go$(GO_VERSION)" || { echo 'Go $(GO_VERSION) required'; exit 1; }
	$(Q)test "$$( $(GO) env CGO_ENABLED )" = 1 || { echo 'CGO required for SQLite and race tests'; exit 1; }
	$(Q)command -v git >/dev/null && command -v bash >/dev/null && command -v tar >/dev/null && command -v "$$( $(GO) env CC | cut -d ' ' -f 1 )" >/dev/null
	$(Q)"$(GOLANGCI_LINT)" version | awk '/version $(LINT_VERSION)( |$$)/ { found=1 } END { exit !found }'

inventory:
	$(Q)cd tools && $(GO) test -count=1 -run '^TestModuleInventory$$' ./...

lint:
	$(Q)status=0; for dir in $(DEVELOPMENT_MODULES); do \
		echo "lint - $$dir"; \
		(cd "$$dir" && test -z "$$(gofmt -l .)") || status=1; \
		(cd "$$dir" && $(GO) vet -mod=readonly -tags=integration ./...) || status=1; \
		(cd "$$dir" && "$(GOLANGCI_LINT)" run --allow-serial-runners --build-tags integration ./...) || status=1; \
	done; exit $$status

test:
	$(Q)status=0; for dir in $(DEVELOPMENT_MODULES); do \
		echo "test - $$dir"; \
		(cd "$$dir" && $(GO) test $(TEST_FLAGS) ./...) || status=1; \
	done; exit $$status

check:
	$(Q)$(MAKE) --no-print-directory environment || exit $$?
	$(Q)$(MAKE) --no-print-directory inventory || exit $$?
	$(Q)status=0; for stage in lint test examples test-integration; do \
		echo "check - $$stage"; \
		if $(MAKE) --no-print-directory $$stage TEST_FLAGS='$(CHECK_TEST_FLAGS)'; then echo "$$stage: PASS"; else echo "$$stage: FAIL"; status=1; fi; \
	done; echo "check: exit $$status"; exit $$status

examples:
	$(Q)build=$$(mktemp -d "$${TMPDIR:-/tmp}/memy-examples.XXXXXXXX"); trap 'rm -rf "$$build"' EXIT HUP INT TERM; \
	status=0; for name in lifecycle quality retrieval quality-integration managed-projections; do \
		echo "build example - $$name"; $(GO) build -mod=readonly -o "$$build/$$name" "./examples/$$name" || status=1; \
	done; exit $$status

test-integration:
	$(Q)status=0; for target in test-candidate test-published; do \
		$(MAKE) --no-print-directory $$target || status=1; \
	done; exit $$status

test-candidate:
	$(Q)cd tools && $(GO) test -mod=readonly -tags=integration -race -count=1 -timeout=$(TEST_TIMEOUT) -run '^TestCandidateArtifacts$$' ./...

release-candidate:
	$(Q)MEMY_CANDIDATE_SOURCE="$(RELEASE_SOURCE)" MEMY_CANDIDATE_VERSION="$(RELEASE_VERSION)" $(MAKE) --no-print-directory test-candidate

test-published:
	$(Q)cd tools && $(GO) test -mod=readonly -tags=integration -race -count=1 -timeout=$(TEST_TIMEOUT) -run '^TestPublishedConsumer$$' ./...

bench:
	$(Q)for dir in $(DEVELOPMENT_MODULES); do (cd "$$dir" && $(GO) test -mod=readonly -bench=. -run='^$$' -benchmem ./...) || exit 1; done

fuzz:
	$(Q)for dir in $(DEVELOPMENT_MODULES); do \
		(cd "$$dir" && packages=$$($(GO) list -mod=readonly ./...) && for pkg in $$packages; do \
			targets=$$($(GO) test -mod=readonly -list '^Fuzz' "$$pkg") || exit 1; \
			for target in $$targets; do \
				case "$$target" in Fuzz*) $(GO) test -mod=readonly -run='^$$' -fuzz="^$$target$$" -fuzztime=$(FUZZTIME) -parallel=$(FUZZPARALLEL) "$$pkg" || exit 1 ;; esac; \
			done; done) || exit 1; \
	done

cover:
	$(Q)for dir in $(DEVELOPMENT_MODULES); do (cd "$$dir" && $(GO) test -mod=readonly -coverprofile=coverage.out ./... && $(GO) tool cover -func=coverage.out) || exit 1; done

release-patch:
	$(Q)bash scripts/release.sh patch
release-break:
	$(Q)bash scripts/release.sh break
